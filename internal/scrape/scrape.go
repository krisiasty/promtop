// Package scrape performs one all-or-nothing HTTP scrape of an exposition
// endpoint and classifies failures with the design's error texts.
package scrape

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/krisiasty/promtop/internal/config"
	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/format"
)

// MaxBody caps the response size; larger payloads fail the scrape.
const MaxBody = 64 << 20

const accept = "application/openmetrics-text;version=1.0.0;q=0.9,text/plain;version=0.0.4;q=0.8,*/*;q=0.1"

// Kind classifies a failed scrape.
type Kind uint8

const (
	KindOther Kind = iota
	KindRefused
	KindTimeout
	KindHTTP
	KindParse
	KindTooLarge
)

// Result is one successful scrape.
type Result struct {
	Parsed      *expo.Parsed
	Status      string // e.g. "200 OK"
	ContentType string
	Bytes       int
	Duration    time.Duration
	At          time.Time // when the request started
}

// Error is one failed scrape. Full is the banner/popup text, Short the
// narrow-width text, Attempt the text in the "last 10 attempts" list.
type Error struct {
	Kind     Kind
	Full     string
	Short    string
	Attempt  string
	Duration time.Duration
	At       time.Time
}

func (e *Error) Error() string { return e.Full }

// Client scrapes one endpoint.
type Client struct {
	http      *http.Client
	url       string
	timeout   time.Duration
	auth      string
	match     *regexp.Regexp
	userAgent string
}

// NewClient builds a client; it fails on unreadable CA or client-cert files.
func NewClient(t config.Target, match *regexp.Regexp, userAgent string) (*Client, error) {
	tlsCfg := &tls.Config{InsecureSkipVerify: t.InsecureSkipVerify} //nolint:gosec // opt-in via flag
	if t.CAFile != "" {
		pemData, err := os.ReadFile(t.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read CA file: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pemData) {
			return nil, fmt.Errorf("CA file %s: no certificates found", t.CAFile)
		}
		tlsCfg.RootCAs = pool
	}
	if t.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load client certificate: %w", err)
		}
		tlsCfg.Certificates = []tls.Certificate{cert}
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.TLSClientConfig = tlsCfg
	c := &Client{http: &http.Client{Transport: tr}, url: t.URL, timeout: t.Timeout, match: match, userAgent: userAgent}
	switch {
	case t.BearerToken != "":
		c.auth = "Bearer " + t.BearerToken
	case t.Username != "":
		c.auth = "Basic " + base64.StdEncoding.EncodeToString([]byte(t.Username+":"+t.Password))
	}
	if c.auth != "" {
		// The default redirect policy can forward Authorization to a different
		// port or from HTTPS to HTTP on the same hostname.
		c.http.CheckRedirect = func(*http.Request, []*http.Request) error {
			return errors.New("redirects are not allowed for authenticated scrapes")
		}
	}
	return c, nil
}

// Scrape performs one request. Any failure — transport, non-2xx status,
// oversized body or parse error — returns an *Error and no partial data.
func (c *Client) Scrape(ctx context.Context) (*Result, error) {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	fail := func(kind Kind, full, short, attempt string) (*Result, error) {
		return nil, &Error{Kind: kind, Full: expo.Sanitize(full), Short: expo.Sanitize(short), Attempt: expo.Sanitize(attempt),
			Duration: time.Since(start), At: start}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return fail(KindOther, err.Error(), err.Error(), err.Error())
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", c.userAgent)
	if c.auth != "" {
		req.Header.Set("Authorization", c.auth)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, c.transportErr(err, start)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10)) // drain for connection reuse; errors are irrelevant here
		full := "HTTP " + resp.Status
		return fail(KindHTTP, full, "HTTP "+strconv.Itoa(resp.StatusCode), full)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
	if err != nil {
		return nil, c.transportErr(err, start)
	}
	if len(body) > MaxBody {
		return fail(KindTooLarge, "response too large (over 64 MiB)", "response too large", "response too large")
	}
	parsed, err := expo.Parse(bytes.NewReader(body), c.match)
	if err != nil {
		var pe *expo.ParseError
		if errors.As(err, &pe) {
			return fail(KindParse, pe.Error()+" · scrape discarded", "parse error line "+strconv.Itoa(pe.Line), pe.Error())
		}
		return fail(KindOther, err.Error(), lastSegment(err.Error()), err.Error())
	}
	return &Result{Parsed: parsed, Status: expo.Sanitize(resp.Status), ContentType: resp.Header.Get("Content-Type"),
		Bytes: len(body), Duration: time.Since(start), At: start}, nil
}

func (c *Client) transportErr(err error, start time.Time) *Error {
	inner := err
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		inner = urlErr.Err
	}
	msg := expo.Sanitize(inner.Error()) // may quote server-supplied text, e.g. certificate names
	e := &Error{Kind: KindOther, Full: msg, Duration: time.Since(start), At: start}
	var ne net.Error
	switch {
	case errors.Is(err, context.DeadlineExceeded) || errors.As(err, &ne) && ne.Timeout():
		t := format.Interval(c.timeout)
		e.Kind, e.Full, e.Short, e.Attempt = KindTimeout, "context deadline exceeded (timeout "+t+")", "timeout "+t, "timeout after "+t
	case errors.Is(err, syscall.ECONNREFUSED) || strings.Contains(strings.ToLower(msg), "refused"):
		e.Kind, e.Short, e.Attempt = KindRefused, "connection refused", "connection refused"
	default:
		e.Short, e.Attempt = lastSegment(msg), msg
	}
	return e
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, ": "); i >= 0 {
		return s[i+2:]
	}
	return s
}
