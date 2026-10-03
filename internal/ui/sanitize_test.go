package ui

import (
	"regexp"
	"strings"
	"testing"
)

// sgrRE matches the colour sequences lipgloss itself emits.
var sgrRE = regexp.MustCompile("\x1b\\[[0-9;:]*m")

// Control sequences in scraped label values, HELP text and raw lines must
// not reach the terminal, in any view that shows them; a label value's \n
// escape must not add a line to the frame either.
func TestScrapedControlCharactersNeverReachTheScreen(t *testing.T) {
	f := newFixture(t, 0)
	payload := "# HELP evil pwn\x1b]0;title\x07 help\x1b[2J\n# TYPE evil gauge\n" +
		"evil{v=\"a\x1b]8;;http://x\x1b\\\\b\u009b31m\\nnext line\"} 1\n# note \x1b[H\x1b[2J\n" +
		"# EOF\n# tail \x1b]8;;https://example.org/\x1b\\click\x1b]8;;\x1b\\\u009b2J\xff\n"
	f.apply(payload)
	f.apply(strings.Replace(payload, "} 1", "} 2", 1))
	for _, w := range []int{80, 146} {
		m := f.model(w, 36)
		for v := ViewTable; v <= ViewRaw; v++ {
			m.view, m.detailOpen, m.famOpen = v, v <= ViewGraph, true
			m.cursor = 0
			out := m.render()
			assertFrame(t, out, w, 36)
			rest := sgrRE.ReplaceAllString(out, "")
			if i := strings.IndexFunc(rest, func(r rune) bool { return r < 0x20 && r != '\n' || r >= 0x7f && r <= 0x9f }); i >= 0 {
				t.Errorf("%s @%d: control %q reaches the screen: …%q", viewNames[v], w, rest[i], rest[max(0, i-20):min(len(rest), i+20)])
			}
		}
	}
}

// A tab is a legal label value character: "a b" and "a<tab>b" are distinct
// series and must look distinct, without a tab reaching the screen.
func TestTabInLabelValueStaysDistinct(t *testing.T) {
	f := newFixture(t, 0)
	payload := "# TYPE m gauge\nm{v=\"a b\"} 1\nm{v=\"a\tb\"} 2\n"
	f.apply(payload)
	f.apply(payload)
	m := f.model(146, 36)
	m.cursor = 0
	if rows := m.rows(); len(rows) != 2 || rows[0].Key == rows[1].Key {
		t.Fatalf("rows = %d, want 2 distinct series", len(rows))
	}
	out := m.render()
	if text := plain(out); !strings.Contains(text, `m{v="a b"}`) || !strings.Contains(text, `m{v="a\tb"}`) {
		t.Errorf("the tab must show as \\t:\n%s", text)
	}
	if strings.ContainsRune(out, '\t') {
		t.Error("a tab reaches the screen")
	}
}
