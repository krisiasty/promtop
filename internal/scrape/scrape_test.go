package scrape

import (
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/krisiasty/promtop/internal/config"
)

func newClient(t *testing.T, tgt config.Target, match *regexp.Regexp) *Client {
	t.Helper()
	if tgt.Timeout == 0 {
		tgt.Timeout = 2 * time.Second
	}
	c, err := NewClient(tgt, match, "promtop/test")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func scrapeErr(t *testing.T, c *Client) *Error {
	t.Helper()
	_, err := c.Scrape(context.Background())
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("err = %v (%T), want *Error", err, err)
	}
	return e
}

func TestScrapeOKAndHeaders(t *testing.T) {
	var got http.Header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = io.WriteString(w, "# TYPE up gauge\nup 1\nhttp_x 2\n")
	}))
	defer ts.Close()
	c := newClient(t, config.Target{URL: ts.URL + "/metrics", Username: "u", Password: "p"}, regexp.MustCompile(`^(?:up)$`))
	r, err := c.Scrape(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "200 OK" || r.ContentType != "text/plain; version=0.0.4" || r.Bytes != 30 ||
		len(r.Parsed.Families) != 1 || r.At.IsZero() || r.Duration <= 0 {
		t.Errorf("result = %+v", r)
	}
	if got.Get("Accept") != accept || got.Get("User-Agent") != "promtop/test" ||
		got.Get("Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("u:p")) {
		t.Errorf("headers = %v", got)
	}
}

func TestBearerToken(t *testing.T) {
	var auth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
	}))
	defer ts.Close()
	if _, err := newClient(t, config.Target{URL: ts.URL, BearerToken: "abc"}, nil).Scrape(context.Background()); err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer abc" {
		t.Errorf("Authorization = %q", auth)
	}
}

func TestAuthenticatedHTTPSRedirectDoesNotDiscloseCredentials(t *testing.T) {
	for _, tc := range []struct {
		name     string
		target   config.Target
		wantAuth string
	}{
		{name: "bearer", target: config.Target{BearerToken: "secret"}, wantAuth: "Bearer secret"},
		{name: "basic", target: config.Target{Username: "u", Password: "secret"},
			wantAuth: "Basic " + base64.StdEncoding.EncodeToString([]byte("u:secret"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			redirectedAuth := make(chan string, 1)
			insecure := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				redirectedAuth <- r.Header.Get("Authorization")
			}))
			defer insecure.Close()

			originalAuth := make(chan string, 1)
			secure := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				originalAuth <- r.Header.Get("Authorization")
				http.Redirect(w, r, insecure.URL, http.StatusFound)
			}))
			defer secure.Close()

			tc.target.URL = secure.URL
			tc.target.InsecureSkipVerify = true
			e := scrapeErr(t, newClient(t, tc.target, nil))
			if e.Kind != KindOther || e.Full != "redirects are not allowed for authenticated scrapes" ||
				strings.Contains(e.Full, "secret") {
				t.Errorf("redirect error = %+v", e)
			}
			select {
			case got := <-originalAuth:
				if got != tc.wantAuth {
					t.Errorf("original Authorization = %q, want %q", got, tc.wantAuth)
				}
			default:
				t.Error("original endpoint did not receive the scrape")
			}
			select {
			case got := <-redirectedAuth:
				t.Errorf("redirect target received Authorization %q", got)
			default:
			}
		})
	}
}

func TestAnonymousRedirectStillWorks(t *testing.T) {
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "up 1\n")
	}))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer source.Close()

	result, err := newClient(t, config.Target{URL: source.URL}, nil).Scrape(context.Background())
	if err != nil || result.Parsed.Samples != 1 {
		t.Fatalf("anonymous redirect: result = %+v, err = %v", result, err)
	}
}

func TestGzip(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			t.Error("transport did not ask for gzip")
		}
		w.Header().Set("Content-Encoding", "gzip")
		gz := gzip.NewWriter(w)
		_, _ = io.WriteString(gz, "up 1\n")
		if err := gz.Close(); err != nil {
			t.Error(err)
		}
	}))
	defer ts.Close()
	r, err := newClient(t, config.Target{URL: ts.URL}, nil).Scrape(context.Background())
	if err != nil || r.Parsed.Samples != 1 {
		t.Fatalf("gzip scrape: %+v %v", r, err)
	}
}

func TestHTTPStatusError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer ts.Close()
	e := scrapeErr(t, newClient(t, config.Target{URL: ts.URL}, nil))
	if e.Kind != KindHTTP || e.Full != "HTTP 503 Service Unavailable" || e.Short != "HTTP 503" || e.Attempt != e.Full {
		t.Errorf("error = %+v", e)
	}
}

func TestTimeout(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	}))
	defer ts.Close()
	e := scrapeErr(t, newClient(t, config.Target{URL: ts.URL, Timeout: 50 * time.Millisecond}, nil))
	if e.Kind != KindTimeout || e.Full != "context deadline exceeded (timeout 50ms)" ||
		e.Short != "timeout 50ms" || e.Attempt != "timeout after 50ms" {
		t.Errorf("error = %+v", e)
	}
}

func TestRefused(t *testing.T) {
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	e := scrapeErr(t, newClient(t, config.Target{URL: "http://" + addr + "/metrics", Timeout: 5 * time.Second}, nil))
	if e.Kind != KindRefused || e.Short != "connection refused" || !strings.Contains(strings.ToLower(e.Full), "refused") ||
		strings.HasPrefix(e.Full, "Get ") {
		t.Errorf("error = %+v", e)
	}
}

func TestParseErrorDiscardsScrape(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "up 1\nbad{x=\"")
	}))
	defer ts.Close()
	e := scrapeErr(t, newClient(t, config.Target{URL: ts.URL}, nil))
	if e.Kind != KindParse || e.Full != "parse error: unexpected end of input at line 2 · scrape discarded" ||
		e.Short != "parse error line 2" || e.Attempt != "parse error: unexpected end of input at line 2" {
		t.Errorf("error = %+v", e)
	}
}

func TestScrapeHTMLBodyIsParseError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html><body>Welcome</body></html>\n")
	}))
	defer ts.Close()
	e := scrapeErr(t, newClient(t, config.Target{URL: ts.URL}, nil))
	if e.Kind != KindParse || e.Short != "parse error line 1" {
		t.Errorf("error = %+v", e)
	}
}

func TestTLS(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "up 1\n")
	}))
	defer ts.Close()
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ts.Certificate().Raw}), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := newClient(t, config.Target{URL: ts.URL, CAFile: ca}, nil).Scrape(context.Background()); err != nil {
		t.Errorf("with CA: %v", err)
	}
	if _, err := newClient(t, config.Target{URL: ts.URL, InsecureSkipVerify: true}, nil).Scrape(context.Background()); err != nil {
		t.Errorf("insecure: %v", err)
	}
	if e := scrapeErr(t, newClient(t, config.Target{URL: ts.URL}, nil)); e.Kind != KindOther || e.Short == "" {
		t.Errorf("untrusted cert: %+v", e)
	}
	if _, err := NewClient(config.Target{URL: ts.URL, CAFile: filepath.Join(t.TempDir(), "missing.pem"), Timeout: time.Second}, nil, "x"); err == nil {
		t.Error("missing CA file must fail at startup")
	}
}

// rawServer answers every request with the raw HTTP response head.
func rawServer(t *testing.T, head string) string {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		conn, buf, err := http.NewResponseController(w).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer func() { _ = conn.Close() }()
		_, _ = buf.WriteString(head + "Content-Length: 5\r\nConnection: close\r\n\r\nup 1\n")
		_ = buf.Flush()
	}))
	t.Cleanup(ts.Close)
	return ts.URL
}

func hasControl(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r < 0x20 || r >= 0x7f && r <= 0x9f }) >= 0
}

func TestServerTextIsSanitized(t *testing.T) {
	url := rawServer(t, "HTTP/1.1 500 bad\x1b]0;pwned\x07\x1b[2J\r\n")
	e := scrapeErr(t, newClient(t, config.Target{URL: url}, nil))
	if hasControl(e.Full) || hasControl(e.Short) || hasControl(e.Attempt) || !strings.HasPrefix(e.Full, "HTTP 500 bad") {
		t.Errorf("error texts carry control characters: %q / %q / %q", e.Full, e.Short, e.Attempt)
	}
	url = rawServer(t, "HTTP/1.1 200 OK\x1b[2J\r\n") // header values with controls are rejected by net/http
	r, err := newClient(t, config.Target{URL: url}, nil).Scrape(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if hasControl(r.Status) || !strings.HasPrefix(r.Status, "200 OK") {
		t.Errorf("status carries control characters: %q", r.Status)
	}
}
