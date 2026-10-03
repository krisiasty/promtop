package config

import (
	"errors"
	"flag"
	"io/fs"
	"strings"
	"testing"
	"time"
)

func parse(env, files map[string]string, args ...string) (Config, error) {
	getenv := func(k string) string { return env[k] }
	read := func(p string) ([]byte, error) {
		if v, ok := files[p]; ok {
			return []byte(v), nil
		}
		return nil, fs.ErrNotExist
	}
	return Parse(args, getenv, read)
}

func TestDefaultsAndURL(t *testing.T) {
	c, err := parse(nil, nil, "http://10.0.3.14:9100/metrics")
	if err != nil {
		t.Fatal(err)
	}
	if c.Target.URL != "http://10.0.3.14:9100/metrics" || c.Interval != time.Second ||
		c.Window != 5*time.Minute || c.Target.Timeout != 2*time.Second ||
		c.HistoryBudgetBytes != 512<<20 || c.Match != nil {
		t.Errorf("config = %+v", c)
	}
	c, err = parse(nil, nil, "https://h:9115/probe?target=x&module=http_2xx", "--interval", "2s")
	if err != nil || c.Target.URL != "https://h:9115/probe?target=x&module=http_2xx" || c.Interval != 2*time.Second {
		t.Errorf("flags after URL / query kept: %+v %v", c, err)
	}
	c, err = parse(nil, nil, "--url", "http://h")
	if err != nil || c.Target.URL != "http://h" {
		t.Errorf("--url: %+v %v", c, err)
	}
}

func TestHistoryBudget(t *testing.T) {
	for _, tc := range []struct {
		flag string
		want int64
	}{
		{"512MiB", 512 << 20},
		{"1GiB", 1 << 30},
		{"2GiB", 2 << 30},
		{"1GB", 1_000_000_000},
	} {
		c, err := parse(nil, nil, "http://h", "--history-budget", tc.flag)
		if err != nil || c.HistoryBudgetBytes != tc.want {
			t.Errorf("--history-budget %s = %d, %v; want %d", tc.flag, c.HistoryBudgetBytes, err, tc.want)
		}
	}
	for _, invalid := range []string{"0MiB", "-1GiB", "2.5GiB", "1", "100TiB", "9223372036854775807GiB"} {
		if _, err := parse(nil, nil, "--history-budget", invalid, "http://h"); err == nil ||
			!strings.Contains(err.Error(), "--history-budget must be a positive size") {
			t.Errorf("--history-budget %s: err = %v", invalid, err)
		}
	}
}

func TestWindowPresets(t *testing.T) {
	for _, d := range []string{"30s", "1m", "2m", "5m", "10m", "15m", "20m", "30m"} {
		if _, err := parse(nil, nil, "--window", d, "http://h"); err != nil {
			t.Errorf("--window %s: %v", d, err)
		}
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, "missing endpoint URL"},
		{[]string{"localhost:9100/metrics"}, "scheme must be http or https"},
		{[]string{"ftp://h/x"}, "scheme must be http or https"},
		{[]string{"10.0.3.14:9100"}, "invalid URL"},
		{[]string{"http:///metrics"}, "missing host"},
		{[]string{"http://u:p@h/metrics"}, "credentials in the URL are not allowed"},
		{[]string{"http://a", "b"}, `unexpected argument "b"`},
		{[]string{"--window", "3m", "http://h"}, "--window must be 30s, 1m, 2m, 5m, 10m, 15m, 20m or 30m"},
		{[]string{"--interval", "50ms", "http://h"}, "--interval must be at least 100ms"},
		{[]string{"--timeout", "0s", "http://h"}, "--timeout must be positive"},
		{[]string{"--match", "(", "http://h"}, "invalid --match"},
		{[]string{"--password-file", "pw", "http://h"}, "--password-file requires --username"},
		{[]string{"--username", "u", "--bearer-token-file", "tok", "http://h"}, "mutually exclusive"},
		{[]string{"--cert-file", "c.pem", "http://h"}, "--cert-file and --key-file must be used together"},
		{[]string{"--bogus", "http://h"}, "flag provided but not defined"},
	}
	files := map[string]string{"pw": "x", "tok": "t"}
	for _, c := range cases {
		_, err := parse(nil, files, c.args...)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%v: err = %v, want it to contain %q", c.args, err, c.want)
		}
	}
}

func TestMatchIsAnchored(t *testing.T) {
	c, err := parse(nil, nil, "--match", "http_.*", "http://h")
	if err != nil {
		t.Fatal(err)
	}
	if !c.Match.MatchString("http_requests_total") || c.Match.MatchString("xhttp_requests_total") {
		t.Error("--match must be anchored")
	}
}

func TestSecrets(t *testing.T) {
	c, err := parse(nil, map[string]string{"pw": "s3cret\r\n"}, "--username", "u", "--password-file", "pw", "http://h")
	if err != nil || c.Target.Username != "u" || c.Target.Password != "s3cret" {
		t.Errorf("password file: %+v %v", c.Target, err)
	}
	c, err = parse(map[string]string{"PROMTOP_PASSWORD": "envpw"}, nil, "--username", "u", "http://h")
	if err != nil || c.Target.Password != "envpw" {
		t.Errorf("password env: %+v %v", c.Target, err)
	}
	c, err = parse(nil, map[string]string{"tok": " abc \n"}, "--bearer-token-file", "tok", "http://h")
	if err != nil || c.Target.BearerToken != "abc" {
		t.Errorf("token file: %+v %v", c.Target, err)
	}
	c, err = parse(map[string]string{"PROMTOP_BEARER_TOKEN": "envtok"}, nil, "http://h")
	if err != nil || c.Target.BearerToken != "envtok" {
		t.Errorf("token env: %+v %v", c.Target, err)
	}
	c, err = parse(map[string]string{"PROMTOP_BEARER_TOKEN": "envtok"}, nil, "--username", "u", "http://h")
	if err != nil || c.Target.BearerToken != "" {
		t.Errorf("env token must not apply with --username: %+v %v", c.Target, err)
	}
	if _, err := parse(nil, nil, "--password-file", "missing", "--username", "u", "http://h"); err == nil {
		t.Error("unreadable password file must fail")
	}
}

func TestVersionAndHelp(t *testing.T) {
	c, err := parse(nil, nil, "--version")
	if err != nil || !c.ShowVersion {
		t.Errorf("--version: %+v %v", c, err)
	}
	if _, err := parse(nil, nil, "-h"); !errors.Is(err, flag.ErrHelp) {
		t.Errorf("-h: %v", err)
	}
}
