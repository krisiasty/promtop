package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

func TestSummaryCountFormatting(t *testing.T) {
	for _, tc := range []struct{ name, unit string }{
		{"latency_seconds", ""}, {"payload_bytes", ""},
		{"latency", "seconds"}, {"payload", "bytes"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 0)
			for _, count := range []int{100, 110} {
				text := fmt.Sprintf("# TYPE %s summary\n", tc.name)
				if tc.unit != "" {
					text += fmt.Sprintf("# UNIT %s %s\n", tc.name, tc.unit)
				}
				text += fmt.Sprintf("%s{quantile=\"0.5\"} 0.01\n%s_sum 1\n%s_count %d\n", tc.name, tc.name, tc.name, count)
				f.apply(text)
			}
			// A count's rate is observations/s in either unit: never B/s.
			rate := f.model(146, 36)
			rate.showRate = true
			if got := countRow(t, rate, tc.name+"_count"); len(got) < 1 || got[0] != "10.0" {
				t.Errorf("count rate = %v, want 10.0 observations/s", got)
			}
			m := f.model(146, 36)
			m.showRate = false
			m.cursor = rowIndex(m, tc.name+"_count")
			if m.cursor < 0 {
				t.Fatal("summary count row is missing")
			}
			if got := countRow(t, m, tc.name+"_count"); len(got) < 2 || got[0] != "110" || got[1] != "100" {
				t.Errorf("raw count row = %v, want current 110 and previous 100", got)
			}
			press(m, "enter")
			popup := plain(m.render())
			if !strings.Contains(popup, "unit: —") {
				t.Error("count details should be dimensionless")
			}
			// Match label/value pairs so other popup text using these words cannot interfere.
			for label, want := range map[string]string{"current": "110", "previous": "100", "mean": "105", "min": "100", "max": "110"} {
				if !regexp.MustCompile(`(^|\s)` + label + `\s+` + want + `(\s|$)`).MatchString(popup) {
					t.Errorf("count %s should be %s:\n%s", label, want, popup)
				}
			}
		})
	}
}

// countRow returns the Table cells after the type tag of the row named name.
func countRow(t *testing.T, m *Model, name string) []string {
	t.Helper()
	for _, line := range plainLines(m.render()) {
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "▸"))
		if len(fields) > 1 && fields[0] == name && fields[1] == "ctr" {
			return fields[2:]
		}
	}
	t.Errorf("table row %s is missing", name)
	return nil
}

func TestTimestampFormatting(t *testing.T) {
	f := newFixture(t, 0)
	lastRun := fixtureBase.Unix() - 3600
	boot := fixtureBase.Unix() - 5*24*3600
	payload := func(run int64) string {
		return fmt.Sprintf("job_last_success_timestamp_seconds %d\nnode_boot_time_seconds %d\n", run, boot)
	}
	f.apply(payload(lastRun))
	f.apply(payload(lastRun + 3))
	m := f.model(146, 36)
	m.cursor = 0
	lines := plainLines(m.render())
	job, bootRow := lines[4], lines[5]
	if !strings.Contains(job, " 11:22:02 ") || !strings.Contains(job, " 11:21:59 ") || !strings.Contains(job, " +3s ") {
		t.Errorf("same-day timestamp row = %q", job)
	}
	if !strings.Contains(bootRow, " Sep 21 ") {
		t.Errorf("older timestamp row = %q", bootRow)
	}
	for _, w := range []int{80, 146, 294} {
		mm := f.model(w, 36)
		assertFrame(t, mm.render(), w, 36)
	}

	press(m, "enter")
	popup := plain(m.render())
	if !strings.Contains(popup, "unit: timestamp") || !strings.Contains(popup, "2026-09-26 11:22:02 · 59m58s ago") {
		t.Errorf("details popup lacks full time:\n%s", popup)
	}
	press(m, "esc", "2")
	if hist := plainLines(m.render()); !strings.Contains(hist[3], "2026-09-26 11:22:02 · 59m58s ago") || !strings.Contains(hist[6], "11:22:02") {
		t.Errorf("history title/rows:\n%s\n%s", hist[3], hist[6])
	}
	press(m, "3")
	if g := plainLines(m.render()); !strings.Contains(g[3], "2026-09-26 11:22:02 · 59m58s ago") {
		t.Errorf("graph title = %q", g[3])
	}
}
