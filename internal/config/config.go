// Package config parses promtop's command line.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Target describes how to reach the endpoint.
type Target struct {
	URL                string
	Timeout            time.Duration
	Username           string
	Password           string
	BearerToken        string
	CAFile             string
	CertFile           string
	KeyFile            string
	InsecureSkipVerify bool
}

// Config is the validated command line.
type Config struct {
	Target             Target
	Interval           time.Duration
	Window             time.Duration
	HistoryBudgetBytes int64
	Match              *regexp.Regexp
	ShowVersion        bool
}

const usage = `Usage: promtop [flags] URL

Scrape one Prometheus endpoint and explore it in a terminal UI.
URL must include the scheme (http:// or https://) and is used exactly as given.

Flags:
  --url string               endpoint (alternative to the positional URL)
  --interval duration        scrape interval (default 1s, minimum 100ms)
  --window duration          initial stats window: 30s, 1m, 2m, 5m, 10m, 15m, 20m or 30m (default 5m)
  --history-budget size      estimated ring memory budget (default 512MiB; e.g. 1GiB or 2GiB)
  --timeout duration         per-scrape timeout (default 2s)
  --match regexp             keep only metric families whose name matches
  --username string          basic auth user (password from --password-file or $PROMTOP_PASSWORD)
  --password-file path       file holding the basic auth password
  --bearer-token-file path   file holding a bearer token (or $PROMTOP_BEARER_TOKEN)
  --ca-file path             extra CA certificates (PEM)
  --cert-file path           client certificate (PEM), requires --key-file
  --key-file path            client key (PEM), requires --cert-file
  --insecure-skip-verify     do not verify the server certificate
  --version                  print the version and exit
`

// Usage writes the help text.
func Usage(w io.Writer) { _, _ = io.WriteString(w, usage) }

// Parse parses and validates args (without the program name). getenv and
// readFile are injected so tests need no real environment or files.
func Parse(args []string, getenv func(string) string, readFile func(string) ([]byte, error)) (Config, error) {
	var (
		c                                                  Config
		urlFlag, match, passFile, tokenFile, historyBudget string
		positional                                         []string
	)
	fs := flag.NewFlagSet("promtop", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&urlFlag, "url", "", "")
	fs.DurationVar(&c.Interval, "interval", time.Second, "")
	fs.DurationVar(&c.Window, "window", 5*time.Minute, "")
	fs.StringVar(&historyBudget, "history-budget", "512MiB", "")
	fs.DurationVar(&c.Target.Timeout, "timeout", 2*time.Second, "")
	fs.StringVar(&match, "match", "", "")
	fs.StringVar(&c.Target.Username, "username", "", "")
	fs.StringVar(&passFile, "password-file", "", "")
	fs.StringVar(&tokenFile, "bearer-token-file", "", "")
	fs.StringVar(&c.Target.CAFile, "ca-file", "", "")
	fs.StringVar(&c.Target.CertFile, "cert-file", "", "")
	fs.StringVar(&c.Target.KeyFile, "key-file", "", "")
	fs.BoolVar(&c.Target.InsecureSkipVerify, "insecure-skip-verify", false, "")
	fs.BoolVar(&c.ShowVersion, "version", false, "")
	for { // allow flags before and after the positional URL
		if err := fs.Parse(args); err != nil {
			return c, err
		}
		if fs.NArg() == 0 {
			break
		}
		positional = append(positional, fs.Arg(0))
		args = fs.Args()[1:]
	}
	if c.ShowVersion {
		return c, nil
	}

	c.Target.URL = urlFlag
	switch {
	case len(positional) > 1:
		return c, fmt.Errorf("unexpected argument %q", positional[1])
	case c.Target.URL == "" && len(positional) == 1:
		c.Target.URL = positional[0]
	case c.Target.URL == "":
		return c, errors.New("missing endpoint URL")
	}
	if err := validateURL(c.Target.URL); err != nil {
		return c, err
	}
	if c.Interval < 100*time.Millisecond {
		return c, errors.New("--interval must be at least 100ms")
	}
	if c.Window != 30*time.Second && c.Window != time.Minute && c.Window != 2*time.Minute &&
		c.Window != 5*time.Minute && c.Window != 10*time.Minute && c.Window != 15*time.Minute &&
		c.Window != 20*time.Minute && c.Window != 30*time.Minute {
		return c, errors.New("--window must be 30s, 1m, 2m, 5m, 10m, 15m, 20m or 30m")
	}
	if c.Target.Timeout <= 0 {
		return c, errors.New("--timeout must be positive")
	}
	var err error
	c.HistoryBudgetBytes, err = parseHistoryBudget(historyBudget)
	if err != nil {
		return c, err
	}
	if match != "" {
		re, err := regexp.Compile("^(?:" + match + ")$")
		if err != nil {
			return c, fmt.Errorf("invalid --match: %v", err)
		}
		c.Match = re
	}

	if passFile != "" && c.Target.Username == "" {
		return c, errors.New("--password-file requires --username")
	}
	if tokenFile != "" && c.Target.Username != "" {
		return c, errors.New("--username and --bearer-token-file are mutually exclusive")
	}
	if c.Target.Username != "" {
		c.Target.Password = getenv("PROMTOP_PASSWORD")
		if passFile != "" {
			b, err := readFile(passFile)
			if err != nil {
				return c, fmt.Errorf("read --password-file: %w", err)
			}
			c.Target.Password = strings.TrimRight(string(b), "\r\n")
		}
	} else {
		c.Target.BearerToken = getenv("PROMTOP_BEARER_TOKEN")
		if tokenFile != "" {
			b, err := readFile(tokenFile)
			if err != nil {
				return c, fmt.Errorf("read --bearer-token-file: %w", err)
			}
			c.Target.BearerToken = strings.TrimSpace(string(b))
		}
	}
	if (c.Target.CertFile == "") != (c.Target.KeyFile == "") {
		return c, errors.New("--cert-file and --key-file must be used together")
	}
	return c, nil
}

func parseHistoryBudget(s string) (int64, error) {
	const invalid = "--history-budget must be a positive size such as 512MiB, 1GiB, or 2GiB"
	for _, unit := range []struct {
		suffix string
		bytes  int64
	}{
		{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"GB", 1_000_000_000}, {"MB", 1_000_000},
	} {
		if len(s) < len(unit.suffix) || !strings.EqualFold(s[len(s)-len(unit.suffix):], unit.suffix) {
			continue
		}
		n, err := strconv.ParseInt(s[:len(s)-len(unit.suffix)], 10, 64)
		if err != nil || n <= 0 || n > (1<<63-1)/unit.bytes {
			return 0, errors.New(invalid)
		}
		return n * unit.bytes, nil
	}
	return 0, errors.New(invalid)
}

func validateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid URL %q: %v", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("invalid URL %q: scheme must be http or https", raw)
	}
	if u.Hostname() == "" {
		return fmt.Errorf("invalid URL %q: missing host", raw)
	}
	if u.User != nil {
		return fmt.Errorf("invalid URL %q: credentials in the URL are not allowed, use --username and --password-file", raw)
	}
	return nil
}
