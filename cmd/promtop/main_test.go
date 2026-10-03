package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	var out, errb bytes.Buffer
	if code := run([]string{"--version"}, &out, &errb); code != 0 || out.String() != "version: dev\ncommit: none\ndate: unknown\n" {
		t.Errorf("--version: %d %q", code, out.String())
	}
	out.Reset()
	if code := run([]string{"-h"}, &out, &errb); code != 0 || !strings.Contains(out.String(), "Usage: promtop") {
		t.Errorf("-h: %d %q", code, out.String())
	}
	errb.Reset()
	if code := run([]string{"localhost:9100"}, &out, &errb); code != 2 ||
		!strings.Contains(errb.String(), "scheme must be http or https") || !strings.Contains(errb.String(), "Usage:") {
		t.Errorf("bad URL: %d %q", code, errb.String())
	}
	errb.Reset()
	if code := run([]string{"--ca-file", "/nonexistent/ca.pem", "http://h"}, &out, &errb); code != 1 {
		t.Errorf("unreadable CA: %d %q", code, errb.String())
	}
}
