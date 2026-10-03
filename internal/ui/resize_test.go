package ui

import (
	"fmt"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// cmdMsgs runs cmd and flattens tea.Sequence/tea.Batch into their messages.
func cmdMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Slice || v.Type().Elem() != reflect.TypeFor[tea.Cmd]() {
		return []tea.Msg{msg}
	}
	var out []tea.Msg
	for i := range v.Len() {
		out = append(out, cmdMsgs(v.Index(i).Interface().(tea.Cmd))...)
	}
	return out
}

// Bubble Tea sets tab stops every 8 columns once, at startup, and its renderer
// moves the cursor with tabs. Terminals such as iTerm2 add no stops for columns
// gained later, so a wider window must get its stops again and a repaint.
func TestWidthChangeResetsTabStops(t *testing.T) {
	m := New(testConfig(), Options{})
	if _, cmd := m.Update(tea.WindowSizeMsg{Width: 200, Height: 63}); cmd != nil {
		t.Errorf("the first size needs no reset (Bubble Tea sets stops at startup): %v", cmdMsgs(cmd))
	}
	if _, cmd := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50}); cmd != nil {
		t.Errorf("a height change needs no reset: %v", cmdMsgs(cmd))
	}
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 272, Height: 63})
	msgs := cmdMsgs(cmd)
	if len(msgs) != 2 || msgs[0] != (tea.RawMsg{Msg: ansi.SetTabEvery8Columns}) || fmt.Sprintf("%T", msgs[1]) != "tea.clearScreenMsg" {
		t.Errorf("a width change must reset tab stops, then repaint: %#v", msgs)
	}
}
