package ui

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "rewrite golden files")

type goldenCase struct {
	name  string
	w     int
	setup func(f *fixture, m *Model)
}

// goldenCases mirror the design states (design/promtop-renders.txt).
var goldenCases = []goldenCase{
	{"21-empty-response", 146, func(f *fixture, m *Model) { f.apply("") }},
	{"01-table-146", 146, nil},
	{"07-table-100", 100, nil},
	{"08-table-80", 80, nil},
	{"16-scrape-degraded", 146, func(f *fixture, m *Model) { f.fail(3) }},
	{"17-scrape-down", 146, func(f *fixture, m *Model) { f.fail(7) }},
	{"19-scrape-down-80", 80, func(f *fixture, m *Model) { f.fail(7) }},
	{"20-counter-reset", 146, func(f *fixture, m *Model) { f.gen.Restart(); f.step() }},
	{"22-high-cardinality", 146, func(f *fixture, m *Model) {
		f.gen.SetHighCardinality(true)
		for range 3 {
			f.step()
		}
	}},
	{"15-series-details-popup", 146, func(f *fixture, m *Model) {
		m.cursor = rowIndex(m, kubeID)
		m.detailOpen = true
	}},
	{"18-scrape-error-details", 146, func(f *fixture, m *Model) {
		f.fail(7)
		m.errOpen = true
	}},
	{"02-history-146", 146, func(f *fixture, m *Model) { m.view = ViewHistory }},
	{"11-history-80", 80, func(f *fixture, m *Model) { m.view = ViewHistory }},
	{"03-graph-146", 146, func(f *fixture, m *Model) { m.view = ViewGraph }},
	{"12-graph-80", 80, func(f *fixture, m *Model) { m.view = ViewGraph }},
	{"04-heatmap-146", 146, func(f *fixture, m *Model) { m.view = ViewHeatmap }},
	{"13-heatmap-80", 80, func(f *fixture, m *Model) { m.view = ViewHeatmap }},
	{"05-series-146", 146, func(f *fixture, m *Model) {
		m.view = ViewSeries
		m.famIdx = familyIndex(m, "http_requests_total")
	}},
	{"09-series-80-list", 80, func(f *fixture, m *Model) {
		m.view = ViewSeries
		m.famIdx = familyIndex(m, "http_requests_total")
	}},
	{"10-series-80-detail", 80, func(f *fixture, m *Model) {
		m.view = ViewSeries
		m.famIdx = familyIndex(m, "http_requests_total")
		m.famOpen = true
	}},
	{"06-raw-146", 146, func(f *fixture, m *Model) { m.view = ViewRaw }},
	{"14-raw-80-wrap", 80, func(f *fixture, m *Model) {
		m.view = ViewRaw
		m.rawWrap = true
	}},
	// more cases are added by later tasks
}

func TestGolden(t *testing.T) {
	for _, c := range goldenCases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t, fixtureSteps)
			m := f.model(c.w, 36)
			if c.setup != nil {
				c.setup(f, m)
			}
			out := m.render()
			assertFrame(t, out, c.w, 36)
			golden(t, c.name, plainFrame(out))
		})
	}
}

func golden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name+".txt")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // path is built from testdata/golden and a fixed case name
	if err != nil {
		t.Fatalf("missing golden %s — run: go test ./internal/ui -run TestGolden -update (%v)", path, err)
	}
	if string(want) != got {
		t.Errorf("%s differs from golden\n--- want\n%s\n--- got\n%s", name, want, got)
	}
}
