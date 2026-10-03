package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func click(m *Model, x, y int) { m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}) }

func wheel(m *Model, down bool) {
	b := tea.MouseWheelUp
	if down {
		b = tea.MouseWheelDown
	}
	m.Update(tea.MouseWheelMsg{Button: b})
}

func TestMouseTabsAndTable(t *testing.T) {
	f := newFixture(t, 30)
	m := f.model(146, 36)
	click(m, 24, 1) // "3 Graph" spans cells 22–30
	if m.view != ViewGraph {
		t.Fatalf("tab click → %v", m.view)
	}
	click(m, 1, 1)
	click(m, 10, 3+1+4) // body row 0 is the header; data row 4
	if m.cursor != 4 {
		t.Fatalf("row click → cursor %d", m.cursor)
	}
	click(m, 10, 3+1+4)
	if !m.detailOpen {
		t.Fatal("double click opens details")
	}
	wheel(m, true)
	if m.labOff != 0 { // row 4 has no labels to scroll
		t.Errorf("labOff = %d", m.labOff)
	}
	m.detailOpen = false
	id := m.rows()[2].Key
	was := m.st.Pinned(id)
	click(m, 1, 3+1+2)
	if m.st.Pinned(id) == was {
		t.Error("mark-column click toggles the pin")
	}
	wheel(m, true)
	if m.cursor != 3 {
		t.Errorf("wheel moves the cursor: %d", m.cursor)
	}
	m.view = ViewRaw
	wheel(m, true)
	if m.rawOff != 3 {
		t.Errorf("wheel scrolls Raw by 3: %d", m.rawOff)
	}
}

func TestMouseSeriesNarrow(t *testing.T) {
	f := newFixture(t, 30)
	m := f.model(80, 36)
	m.view = ViewSeries
	click(m, 5, 3+1+2)
	if m.famIdx != 2 {
		t.Fatalf("family click → %d", m.famIdx)
	}
	click(m, 5, 3+1+2)
	if !m.famOpen {
		t.Error("clicking the selected family opens it")
	}
}
