package demo

import (
	"strings"
	"testing"

	"github.com/krisiasty/promtop/internal/expo"
)

func parse(t *testing.T, text string) *expo.Parsed {
	t.Helper()
	p, err := expo.Parse(strings.NewReader(text), nil)
	if err != nil {
		t.Fatalf("demo output does not parse: %v", err)
	}
	return p
}

func TestDemoOutput(t *testing.T) {
	g := New(1)
	p := parse(t, g.Step())
	if p.Families[0].Name != "up" || p.Families[7].Name != "http_requests_total" ||
		p.Families[8].Name != "http_request_duration_seconds" || p.Families[8].Type != expo.Histogram ||
		p.Families[len(p.Families)-1].Name != "process_start_time_seconds" {
		t.Errorf("family order wrong: %v …", p.Families[0].Name)
	}
	if len(p.Families[8].Samples) != 13 {
		t.Errorf("histogram samples = %d, want 13", len(p.Families[8].Samples))
	}
	if a, b := New(1).Step(), New(1).Step(); a != b {
		t.Error("same seed must give the same output")
	}
}

func TestDemoRestartDropsCounters(t *testing.T) {
	g := New(1)
	for range 10 {
		g.Step()
	}
	before := parse(t, g.Render())
	g.Restart()
	after := parse(t, g.Step())
	b, a := before.Families[5].Samples[0].Value, after.Families[5].Samples[0].Value // process_cpu_seconds_total
	if a >= b {
		t.Errorf("counter did not drop: %v → %v", b, a)
	}
}

func TestDemoModes(t *testing.T) {
	g := New(1)
	g.SetHighCardinality(true)
	p := parse(t, g.Step())
	if last := p.Families[len(p.Families)-1]; last.Name != "tenant_api_requests_total" || len(last.Samples) != 20000 {
		t.Errorf("high cardinality family = %s (%d)", last.Name, len(last.Samples))
	}
	g.SetEmpty(true)
	if g.Step() != "" {
		t.Error("empty mode must render nothing")
	}
}
