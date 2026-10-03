package ui

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/krisiasty/promtop/internal/format"
)

func TestRawLayout(t *testing.T) {
	f := newFixture(t, 30)
	m := f.model(146, 36)
	m.view = ViewRaw
	out := m.render()
	assertFrame(t, out, 146, 36)
	lines := plainLines(out)
	if !strings.HasPrefix(lines[3], "╭─ last scrape · text exposition format ─") || !strings.HasSuffix(lines[3], "╮") {
		t.Errorf("title = %q", lines[3])
	}
	for i, want := range []string{
		"│    1  # HELP up 1 if the target is reachable, 0 otherwise.",
		"│    2  # TYPE up gauge",
		"│    3  up 1",
	} {
		if !strings.HasPrefix(lines[4+i], want) || !strings.HasSuffix(lines[4+i], "│") {
			t.Errorf("row %d = %q", i, lines[4+i])
		}
	}
	if !strings.Contains(lines[31], " more ─╯") {
		t.Errorf("bottom marker = %q", lines[31])
	}
	if !strings.HasPrefix(lines[34], "rows 1–27 of ") || !strings.Contains(lines[34], " lines · changed samples highlighted") {
		t.Errorf("status = %q", lines[34])
	}
	press(m, "down", "down", "down")
	if lines = plainLines(m.render()); !strings.Contains(lines[3], " ▲ 3 more ─╮") || !strings.HasPrefix(lines[4], "│    4  ") {
		t.Errorf("after scrolling: %q / %q", lines[3], lines[4])
	}
	press(m, "G")
	if lines = plainLines(m.render()); strings.Contains(lines[31], "more") {
		t.Errorf("G must reach the end: %q", lines[31])
	}
}

func TestRawScrollingAfterNarrowResize(t *testing.T) {
	f := newFixture(t, 0)
	f.apply("# HELP up " + strings.Repeat("界", 100) + "\n" + strings.Repeat("# comment\n", 40) + "up 1\n")
	for _, wrap := range []bool{false, true} {
		for _, width := range []int{0, 10, 11, 12, 13, minW - 1} {
			t.Run(fmt.Sprintf("wrap=%t/width=%d", wrap, width), func(t *testing.T) {
				m := f.model(80, 36)
				m.view, m.rawWrap = ViewRaw, wrap
				press(m, "down")
				m.Update(tea.WindowSizeMsg{Width: width, Height: 36})
				press(m, "down", "up", "pgdown", "pgup", "g", "G")
				wheel(m, true)
				wheel(m, false)
				if m.rawOff != 0 {
					t.Errorf("scroll offset in a too-small terminal = %d, want 0", m.rawOff)
				}
				m.Update(tea.WindowSizeMsg{Width: 80, Height: 36})
				press(m, "down")
				wheel(m, true)
				if m.rawOff != 4 {
					t.Errorf("scroll offset after restoring width = %d, want 4", m.rawOff)
				}
				assertFrame(t, m.render(), 80, 36)
			})
		}
	}
}

func TestRawLayoutAtSmallContentWidths(t *testing.T) {
	f := newFixture(t, 0)
	f.apply("# HELP up 界界abc\nup 1\n")
	for _, wrap := range []bool{false, true} {
		m := f.model(80, 36)
		m.rawWrap = wrap
		for _, width := range []int{-1, 0, 1, 2} {
			rows := m.rawRows(width)
			if width <= 0 && len(rows) != 0 {
				t.Errorf("wrap=%t width=%d: got %d rows, want none", wrap, width, len(rows))
			}
			if width > 0 && len(rows) == 0 {
				t.Errorf("wrap=%t width=%d: missing rows", wrap, width)
			}
			for _, row := range rows {
				var text string
				for _, s := range row.segs {
					text += s.text
				}
				if got := format.Width(text); got > width {
					t.Errorf("wrap=%t width=%d: row %q occupies %d cells", wrap, width, text, got)
				}
			}
		}
	}
}

func TestWrapSegsWideRunes(t *testing.T) {
	segs := []seg{{text: "a界"}, {text: "界b"}}
	for _, tc := range []struct {
		width int
		want  string
	}{{-1, ""}, {0, ""}, {1, "a|�|�|b"}, {2, "a|界|界|b"}, {3, "a界|界b"}} {
		var lines []string
		for _, row := range wrapSegs(segs, tc.width) {
			var text string
			for _, s := range row {
				text += s.text
			}
			lines = append(lines, text)
		}
		if got := strings.Join(lines, "|"); got != tc.want {
			t.Errorf("width=%d: wrapped text = %q, want %q", tc.width, got, tc.want)
		}
	}
}

func TestRawChangedValuesAndWrap(t *testing.T) {
	f := newFixture(t, 0)
	long := "# HELP a " + strings.Repeat("very long help text ", 6)
	f.apply(long + "\n# TYPE a gauge\na 5\nb{x=\"}\"} 1\n")
	f.apply(long + "\n# TYPE a gauge\na 7\nb{x=\"}\"} 1\n")
	m := f.model(80, 36)
	m.view = ViewRaw
	lines := plainLines(m.render())
	if !strings.HasPrefix(lines[6], "│    3  a 7   +2") || !strings.HasPrefix(lines[7], `│    4  b{x="}"} 1`) || strings.Contains(lines[7], "+") {
		t.Errorf("changed value rows:\n%s\n%s", lines[6], lines[7])
	}
	if !strings.HasSuffix(lines[4], "›  │") {
		t.Errorf("clipped line must end with › : %q", lines[4])
	}
	press(m, "w")
	lines = plainLines(m.render())
	if !strings.Contains(lines[3], "· wrap") || !strings.HasPrefix(lines[5], "│      ↪") {
		t.Errorf("wrap:\n%s\n%s", lines[3], lines[5])
	}
}

func TestReorderedLabelsKeepSelectionPinAndRawHighlight(t *testing.T) {
	f := newFixture(t, 0)
	f.apply("# TYPE m counter\nm{a=\"1\",b=\"2\"} 10\n")
	m := f.model(80, 36)
	m.setCursor(m.rows(), 0)
	selected, _ := m.selected(m.rows())
	m.st.SetPinned(selected.Key, true)

	f.apply("# TYPE m counter\nm{b=\"2\",a=\"1\"} 11\n")
	m.clampCursor()
	selectedAfter, _ := m.selected(m.rows())
	if selectedAfter != selected || !m.st.Pinned(selectedAfter.Key) ||
		selectedAfter.ID != `m{b="2",a="1"}` {
		t.Errorf("selection, pin, or display did not follow reordered labels: %+v", selectedAfter)
	}

	segments := m.rawSegments()
	var line string
	for _, segment := range segments[1] {
		line += segment.text
	}
	if want := `m{b="2",a="1"} 11   +1`; line != want {
		t.Errorf("Raw changed-value line = %q, want %q", line, want)
	}
}

// Raw rows are laid out once per scrape, width, wrap mode and theme: frames
// and scroll steps in between reuse them.
func TestRawRowsCached(t *testing.T) {
	f := newFixture(t, 30)
	m := f.model(146, 36)
	m.view = ViewRaw
	a := m.rawRows(135)
	if b := m.rawRows(135); &b[0] != &a[0] {
		t.Fatal("a second call at the same key recomputed the raw rows")
	}
	press(m, "down", "down")
	m.render()
	if b := m.rawRows(135); &b[0] != &a[0] {
		t.Fatal("scrolling and rendering must reuse the raw rows")
	}
	f.step()
	b := m.rawRows(135)
	if &b[0] == &a[0] {
		t.Fatal("a new scrape must recompute the raw rows")
	}
	if c := m.rawRows(69); &c[0] == &b[0] {
		t.Error("a width change must recompute the raw rows")
	}
	for name, change := range map[string]func(){
		"wrap":  func() { press(m, "w") },
		"theme": func() { m.th = newTheme(false) },
	} {
		b = m.rawRows(135)
		change()
		if c := m.rawRows(135); &c[0] == &b[0] {
			t.Errorf("a %s change must recompute the raw rows", name)
		}
	}
}

func TestRawLabelsAfterBlank(t *testing.T) {
	f := newFixture(t, 0)
	f.apply("m {a=\"1\"} 1\n")
	f.apply("m {a=\"1\"} 2\n")
	m := f.model(80, 36)
	segs := m.rawSegments()[0]
	if len(segs) < 2 || segs[0].text != "m" || segs[1].text != ` {a="1"}` || segs[1].st.GetForeground() != m.th.prev.GetForeground() {
		var texts []string
		for _, s := range segs {
			texts = append(texts, s.text)
		}
		t.Errorf("labels after a blank keep the label style: %q", texts)
	}
}
