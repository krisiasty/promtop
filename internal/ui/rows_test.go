package ui

import (
	"testing"

	"github.com/krisiasty/promtop/internal/store"
)

// rows is rebuilt only when the scrape, filter, pins, sort, window or rate
// mode change, not on every key press and frame.
func TestRowsCachedUntilInputsChange(t *testing.T) {
	f := newFixture(t, 30)
	m := f.model(146, 36)
	m.sort = SortCurrent
	same := func(a, b []*store.Series) bool { return len(a) > 0 && len(b) > 0 && &a[0] == &b[0] }
	rows := m.rows()
	if !same(rows, m.rows()) {
		t.Fatal("rows rebuilt without a change")
	}
	m.onlyPinned = true
	pinned := m.rows()
	if same(rows, pinned) || len(pinned) >= len(rows) {
		t.Fatalf("pinned-only must rebuild: %d of %d rows", len(pinned), len(rows))
	}
	m.st.SetPinned(rows[0].Key, !m.st.Pinned(rows[0].Key))
	if again := m.rows(); same(pinned, again) || len(again) == len(pinned) {
		t.Errorf("a pin change must rebuild the pinned-only rows: %d → %d", len(pinned), len(again))
	}
	for name, change := range map[string]func(){
		"filter": func() { m.filter = "go_" },
		"sort":   func() { m.sort = SortP99 },
		"window": func() { m.window = stepWindow(m.window, -1) },
		"rate":   func() { m.showRate = !m.showRate },
		"scrape": func() { f.step() },
	} {
		before := m.rows()
		change()
		if same(before, m.rows()) {
			t.Errorf("%s change must rebuild rows", name)
		}
	}
}
