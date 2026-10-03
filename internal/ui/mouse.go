package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

const doubleClick = 400 * time.Millisecond

func (m *Model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	m.dropHiddenPopups()
	mo := msg.Mouse()
	if mo.Button != tea.MouseLeft || m.detailOpen || m.errOpen || m.w < minW || m.h < minH {
		return m, nil
	}
	if mo.Y == 1 {
		if v, ok := m.tabAt(mo.X); ok {
			m.view = v
		}
		return m, nil
	}
	y := mo.Y - m.bodyTop()
	switch m.view {
	case ViewTable:
		rows := m.rows()
		vis := m.bodyHeight() - 1
		_, cur := m.selected(rows)
		idx := clampOffset(cur, vis, len(rows)) + y - 1
		if y < 1 || y > vis || idx < 0 || idx >= len(rows) {
			return m, nil
		}
		if mo.X < 3 {
			m.setCursor(rows, idx)
			m.togglePin()
			return m, nil
		}
		now := time.Now()
		double := m.lastClick.idx == idx && now.Sub(m.lastClick.at) < doubleClick
		m.setCursor(rows, idx)
		m.lastClick = clickInfo{idx, now}
		if double {
			m.detailOpen, m.labOff = true, 0
		}
	case ViewSeries:
		wide := m.w >= 146
		fams := m.st.Families()
		if !wide && m.famOpen || wide && mo.X >= familyPaneWidth(m.w, fams) {
			return m, nil
		}
		vis := m.bodyHeight() - 4
		idx := clampOffset(m.famIdx, vis, len(fams)) + y - 1
		if y < 1 || y > vis || idx < 0 || idx >= len(fams) {
			return m, nil
		}
		if !wide && idx == m.famIdx {
			m.famOpen = true
		} else {
			m.famIdx = idx
		}
	}
	return m, nil
}

func (m *Model) handleWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	m.dropHiddenPopups()
	d := 0
	switch msg.Mouse().Button {
	case tea.MouseWheelUp:
		d = -1
	case tea.MouseWheelDown:
		d = 1
	}
	switch {
	case d == 0 || m.errOpen:
	case m.detailOpen:
		m.labOff = clamp(m.labOff+d, 0, m.labMax())
	case m.view == ViewRaw:
		m.move(3 * d)
	default:
		m.move(d)
	}
	m.clampCursor()
	return m, nil
}
