package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func isQuit(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	_, ok := cmd().(tea.QuitMsg)
	return ok
}

func TestViewSwitching(t *testing.T) {
	m := newFixture(t, 3).model(146, 36)
	press(m, "left")
	if m.view != ViewRaw {
		t.Errorf("left from Table wraps to Raw, got %v", m.view)
	}
	press(m, "tab")
	if m.view != ViewTable {
		t.Errorf("tab wraps to Table, got %v", m.view)
	}
	press(m, "3")
	if m.view != ViewGraph {
		t.Errorf("3 → Graph, got %v", m.view)
	}
	press(m, "shift+tab", "right", "right")
	if m.view != ViewHeatmap {
		t.Errorf("got %v", m.view)
	}
}

func TestFilterTyping(t *testing.T) {
	m := newFixture(t, 3).model(146, 36)
	press(m, "/", "h", "t", "t", "p", "backspace")
	if !m.filtering || m.filter != "htt" {
		t.Fatalf("filter = %q filtering=%v", m.filter, m.filtering)
	}
	press(m, "q") // typed, not quit
	if m.filter != "httq" {
		t.Errorf("q must be typed while filtering: %q", m.filter)
	}
	press(m, "enter")
	if m.filtering || m.filter != "httq" {
		t.Error("enter keeps the filter")
	}
	press(m, "esc")
	if m.filter != "" {
		t.Error("esc clears the filter")
	}
	press(m, "/", "x", "esc")
	if m.filtering || m.filter != "" {
		t.Error("esc while typing clears and exits")
	}
}

func TestPopupPrecedence(t *testing.T) {
	f := newFixture(t, 3)
	m := f.model(146, 36)
	press(m, "enter")
	if !m.detailOpen {
		t.Fatal("enter opens details in Table")
	}
	press(m, "a", "s")
	if m.onlyPinned || m.sort != SortName {
		t.Error("other keys are ignored while the popup is open")
	}
	if isQuit(press(m, "q")) || m.detailOpen {
		t.Error("q closes the popup instead of quitting")
	}
	press(m, "enter", "2")
	if m.detailOpen || m.view != ViewHistory {
		t.Error("view keys close the popup and switch")
	}
	press(m, "e")
	if m.errOpen {
		t.Error("e only works while scrapes fail")
	}
	f.fail(1)
	press(m, "e")
	if !m.errOpen {
		t.Error("e opens the error popup while failing")
	}
	press(m, "esc")
	if m.errOpen {
		t.Error("esc closes the error popup")
	}
}

func TestModeKeys(t *testing.T) {
	m := newFixture(t, 3).model(146, 36)
	press(m, "w")
	if m.window != 10*time.Minute {
		t.Errorf("w cycles 5m → 10m, got %v", m.window)
	}
	press(m, "w")
	if m.window != 15*time.Minute {
		t.Errorf("w cycles 10m → 15m, got %v", m.window)
	}
	press(m, "6", "w")
	if !m.rawWrap || m.window != 15*time.Minute {
		t.Error("w toggles wrap in Raw and leaves the window alone")
	}
	press(m, "1", "s", "s")
	if m.sort != SortDeltaPct {
		t.Errorf("sort = %v", m.sort)
	}
	press(m, "+")
	if m.window != 20*time.Minute {
		t.Errorf("+ advances the window: %v", m.window)
	}
	press(m, "-")
	if m.window != 15*time.Minute {
		t.Errorf("- reduces the window: %v", m.window)
	}
	for range 20 {
		press(m, "+")
	}
	if m.window != 30*time.Minute {
		t.Errorf("+ must stop at 30m: %v", m.window)
	}
	for range 20 {
		press(m, "-")
	}
	if m.window != 30*time.Second {
		t.Errorf("- must stop at 30s: %v", m.window)
	}
	press(m, "m")
	if m.heatBySeries {
		t.Error("m only works in Heatmap")
	}
	press(m, "4", "m")
	if !m.heatBySeries {
		t.Error("m toggles heatmap mode")
	}
	press(m, "r")
	if m.showRate {
		t.Error("r toggles rate")
	}
	if !isQuit(press(m, "q")) || !isQuit(press(m, "ctrl+c")) {
		t.Error("q and ctrl+c quit")
	}
}

func TestPageKeysOnScrollableLists(t *testing.T) {
	f := newFixture(t, 40)
	m := f.model(146, 36)
	start := m.cursor
	press(m, "pgdown")
	if m.cursor <= start {
		t.Errorf("Table PgDn did not advance: %d", m.cursor)
	}
	press(m, "pgup")
	if m.cursor != start {
		t.Errorf("Table PgUp returned to %d, want %d", m.cursor, start)
	}
	m.view = ViewRaw
	press(m, "pgdown")
	if m.rawOff != m.pageSize() {
		t.Errorf("Raw PgDn offset = %d, want %d", m.rawOff, m.pageSize())
	}
	press(m, "pgup")
	if m.rawOff != 0 {
		t.Errorf("Raw PgUp offset = %d", m.rawOff)
	}

	var payload strings.Builder
	for i := range 50 {
		fmt.Fprintf(&payload, "metric_%02d 1\n", i)
	}
	f.apply(payload.String())
	m.view = ViewSeries
	m.famIdx = 0
	press(m, "pgdown")
	if m.famIdx != m.pageSize() {
		t.Errorf("Series PgDn index = %d, want %d", m.famIdx, m.pageSize())
	}
	press(m, "pgup")
	if m.famIdx != 0 {
		t.Errorf("Series PgUp index = %d", m.famIdx)
	}

	h := ghwFixture(t).model(146, 36)
	h.cursor = rowIndex(h, cpuProbe)
	press(h, "enter", "pgdown")
	if h.labOff == 0 {
		t.Error("histogram details PgDn did not scroll buckets")
	}
	press(h, "pgup")
	if h.labOff != 0 {
		t.Errorf("histogram details PgUp offset = %d", h.labOff)
	}
}

func TestCursorClampsWhenListShrinks(t *testing.T) {
	f := newFixture(t, 10)
	m := f.model(146, 36)
	press(m, "a")
	if n := len(m.rows()); n != 6 {
		t.Fatalf("pinned-only rows = %d", n)
	}
	press(m, "G")
	last := m.rows()[5].Key
	press(m, "space")
	if len(m.rows()) != 5 || m.cursor != 4 || m.st.Pinned(last) {
		t.Errorf("unpinning the last row: rows=%d cursor=%d", len(m.rows()), m.cursor)
	}
	f.apply("up 1\n")
	assertFrame(t, m.render(), 146, 36)
	press(m, "down")
	if m.cursor != 0 {
		t.Errorf("cursor after the list emptied = %d", m.cursor)
	}
}

// The selection follows the series, not the row index: with a value sort the
// rows reorder every scrape, and the open details popup must not switch series.
func TestSelectionFollowsSeriesAcrossScrapes(t *testing.T) {
	f := newFixture(t, 30)
	m := f.model(146, 36)
	press(m, "s", "s") // name → current → Δ%
	if m.sort != SortDeltaPct {
		t.Fatalf("sort = %v", m.sort)
	}
	press(m, "down", "down", "enter")
	s0, _ := m.selected(m.rows())
	if !m.detailOpen || s0 == nil {
		t.Fatal("details popup must be open on a series")
	}
	moved := 0
	for range 20 {
		f.step()
		if rowIndex(m, s0.ID) != 3 { // selected at row 1+2
			moved++
		}
		if s, _ := m.selected(m.rows()); s == nil || s.ID != s0.ID {
			t.Fatalf("after %d scrapes the popup shows %v, want %s", f.n-30, s.ID, s0.ID)
		}
	}
	if moved == 0 {
		t.Fatal("test needs a sort that reorders the rows")
	}
	press(m, "esc", "down", "up")
	if s, _ := m.selected(m.rows()); s.ID != s0.ID {
		t.Errorf("↓ ↑ after reordering selects %s, want %s", s.ID, s0.ID)
	}
}
