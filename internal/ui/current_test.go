package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

// rowCells returns the Table cells after the type tag of the row named name.
func rowCells(m *Model, name string) []string {
	for _, line := range plainLines(m.render()) {
		fields := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "▸"))
		if len(fields) > 2 && fields[0] == name {
			return fields[2:]
		}
	}
	return nil
}

// graphCurrent returns the value at the end of Graph's title and after "cur"
// in its legend, found by content rather than by line position.
func graphCurrent(m *Model) (title, legend string) {
	for _, line := range plainLines(m.render()) {
		fields := strings.Fields(line)
		switch {
		case len(fields) == 0:
		case strings.Contains(line, " · window "):
			title = fields[len(fields)-1]
		case fields[0] == "cur" && len(fields) > 1:
			legend = fields[1]
		}
	}
	return title, legend
}

// hasPair reports whether text shows label followed by value, as stats grids do.
func hasPair(text, label, value string) bool {
	return regexp.MustCompile(`(^|\s)` + regexp.QuoteMeta(label) + `\s+` + regexp.QuoteMeta(value) + `(\s|$)`).MatchString(text)
}

func TestLatestNaNCurrentAcrossViews(t *testing.T) {
	for _, tc := range []struct {
		name, typ string
		rate      bool
		current   []string
		previous  []string // the latest number before an exposed NaN
	}{
		{"gauge", "gauge", false, []string{"3.00", "7.00", "—", "—", "9.00"}, []string{"—", "3.00", "7.00", "7.00", "7.00"}},
		{"counter raw", "counter", false, []string{"3.00", "7.00", "—", "—", "9.00"}, []string{"—", "3.00", "7.00", "7.00", "7.00"}},
		{"counter rate", "counter", true, []string{"—", "4.00", "—", "—", "0.667"}, []string{"—", "—", "4.00", "4.00", "4.00"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, 0)
			for i, value := range []string{"3", "7", "NaN", "NaN", "9"} {
				f.apply(fmt.Sprintf("# TYPE g %s\ng %s\n", tc.typ, value))
				m := f.model(146, 36)
				m.cursor, m.showRate = 0, tc.rate
				if got := rowCells(m, "g"); len(got) < 1 || got[0] != tc.current[i] {
					t.Errorf("scrape %d Table CURRENT = %v, want %s", i, got, tc.current[i])
				}
				m.view = ViewGraph
				if title, legend := graphCurrent(m); title != tc.current[i] || legend != tc.current[i] {
					t.Errorf("scrape %d Graph title/legend current = %s/%s, want %s", i, title, legend, tc.current[i])
				}
				if i == 2 || i == 3 {
					mean := "5.00"
					if tc.rate {
						mean = "4.00"
					}
					if !hasPair(plain(m.render()), "avg", mean) {
						t.Errorf("scrape %d NaN changed the window average, want avg %s", i, mean)
					}
				}
				m.view = ViewHistory
				history := plain(m.render())
				m.view = ViewTable
				press(m, "enter")
				popup := plain(m.render())
				for view, text := range map[string]string{"History": history, "popup": popup} {
					if !hasPair(text, "current", tc.current[i]) || !hasPair(text, "previous", tc.previous[i]) {
						t.Errorf("scrape %d %s current/previous should be %s/%s:\n%s", i, view, tc.current[i], tc.previous[i], text)
					}
				}
			}
		})
	}
}

// A histogram row's interval without observations is stored as NaN, but it is
// not an exposed NaN: every view keeps the last observed mean.
func TestIdleHistogramIntervalKeepsCurrent(t *testing.T) {
	f := newFixture(t, 0)
	for _, count := range []int{1, 3, 3} {
		f.apply(fmt.Sprintf("# TYPE h histogram\nh_bucket{le=\"+Inf\"} %d\nh_sum %d\nh_count %d\n", count, 2*count-1, count))
	}
	m := f.model(146, 36)
	m.cursor = 0
	if got := rowCells(m, "h"); len(got) < 1 || got[0] != "2.00" {
		t.Errorf("Table CURRENT = %v, want the window mean 2.00", got)
	}
	m.view = ViewGraph
	if title, legend := graphCurrent(m); title != "2.00" || legend != "2.00" {
		t.Errorf("Graph title/legend current = %s/%s, want the last interval mean 2.00", title, legend)
	}
	m.view = ViewHistory
	if text := plain(m.render()); !hasPair(text, "current", "2.00") {
		t.Errorf("History current should be the last interval mean 2.00:\n%s", text)
	}
}

// Sorting by current keeps a row whose latest sample is NaN where its latest
// number puts it, so intermittent NaNs (summary quantiles) do not reorder rows.
func TestSortCurrentKeepsExposedNaNInPlace(t *testing.T) {
	f := newFixture(t, 0)
	m := f.model(146, 36)
	m.sort = SortCurrent
	order := func() string {
		var names []string
		for _, s := range m.rows() {
			names = append(names, s.Name)
		}
		return strings.Join(names, " ")
	}
	for i, payload := range []string{"a 5\nb 7\nc 1\n", "a 5\nb NaN\nc 1\n", "a 5\nb 7\nc 1\n"} {
		f.apply("# TYPE a gauge\n# TYPE b gauge\n# TYPE c gauge\n" + payload)
		if got := order(); got != "b a c" {
			t.Errorf("scrape %d order = %s, want b a c", i, got)
		}
	}
}
