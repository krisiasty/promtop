package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/krisiasty/promtop/internal/format"
	"github.com/krisiasty/promtop/internal/store"
)

const kubeID = `kube_pod_container_status_restarts_total{cluster="prod-eu-1",region="eu-central-1",zone="eu-central-1b",namespace="payments",pod="payments-api-7c9f8d6b54-x2kqz",container="api",image="registry.internal/payments/api:2.14.3",image_id="sha256:4f1c9e0a7d2b",container_id="containerd://9b1e44c07a3f",node="ip-10-0-3-14.ec2.internal",host_ip="10.0.3.14",pod_ip="10.42.7.118",uid="e3b0c442-98fc-4c14-9afb-f4c8996fb924",created_by_kind="ReplicaSet",created_by_name="payments-api-7c9f8d6b54",service_account="payments-api",priority_class="high-priority",qos_class="Burstable",team="checkout",env="production"}`

func TestDetailsPopup(t *testing.T) {
	f := newFixture(t, 30)
	m := f.model(146, 36)
	m.cursor = rowIndex(m, kubeID)
	if m.cursor < 0 {
		t.Fatal("kube series missing from the demo")
	}
	rows := m.rows()
	s, idx := m.selected(rows)
	pop := m.detailsPopup(layout{146, 30}, s, idx, len(rows))
	if len(pop) != 22 {
		t.Fatalf("popup height = %d, want 22", len(pop))
	}
	for i, ln := range pop {
		if format.Width(ln) != 106 {
			t.Fatalf("popup line %d is %d cells", i, format.Width(ln))
		}
	}
	p := make([]string, len(pop))
	for i := range pop {
		p[i] = plain(pop[i])
	}
	checks := []struct {
		line int
		want string
	}{
		{0, "╭─ series details "},
		{1, "│ kube_pod_container_status_restarts_total{…} "},
		{2, "│ counter  unit: — "},
		{3, "│ # HELP Number of container restarts per container. "},
		{5, "│ ╭─ window 5m · rate/s ─"},
		{6, "│ │ current "},
		{11, "│ ╭─ labels · 20 ─"},
		{12, "│ │ cluster          \"prod-eu-1\" "},
		{19, "│ " + strings.Repeat("─", 102) + " │"},
		{20, "│ enter/esc close   space pin  ↑ ↓ scroll labels "},
	}
	for _, c := range checks {
		if !strings.HasPrefix(p[c.line], c.want) {
			t.Errorf("line %d = %q, want prefix %q", c.line, p[c.line], c.want)
		}
	}
	if !strings.HasSuffix(p[0], fmt.Sprintf(" %d / %d ─╮", idx+1, len(rows))) || !strings.HasSuffix(p[18], " ▼ 14 more ─╯ │") {
		t.Errorf("border markers: %q / %q", p[0], p[18])
	}
	press(m, "enter", "down", "down")
	pop = m.detailsPopup(layout{146, 30}, s, idx, len(rows))
	if top, bot := plain(pop[11]), plain(pop[18]); !strings.HasSuffix(top, " ▲ 2 more ─╮ │") || !strings.HasSuffix(bot, " ▼ 12 more ─╯ │") {
		t.Errorf("after scrolling: %q / %q", top, bot)
	}
	press(m, "pgdown", "pgdown", "pgdown")
	if m.labOff != 14 {
		t.Errorf("label scroll must stop at the end: %d", m.labOff)
	}
	out := m.render()
	assertFrame(t, out, 146, 36)
	lines := plainLines(out)
	top := lines[3+4] // body row 4 = (30-22)/2
	if i := strings.Index(top, "╭─ series details"); i < 0 || format.Width(top[:i]) != 20 {
		t.Errorf("popup not centred: %q", top)
	}
}

func TestErrorPopup(t *testing.T) {
	f := newFixture(t, 30)
	f.fail(7)
	m := f.model(146, 36)
	pop := m.errorPopup(layout{146, 28})
	var text []string
	for _, ln := range pop {
		text = append(text, plain(ln))
	}
	all := strings.Join(text, "\n")
	for _, want := range []string{
		"╭─ scrape error ─",
		"│ target        http://10.0.3.14:9100/metrics ",
		"│ state         DOWN ",
		"│ error         " + refusedFull + " ",
		"│ last success  12:22:28 (7s ago) ",
		"│ failures      7 consecutive ",
		"│ next retry    in 8s ",
		"│ backoff       every 1s for 4 attempts, then 2s → 4s → 8s … max 30s ",
		"│ timeout       2s ",
		"│ ╭─ last 10 attempts ─",
		"│ │ 12:22:35  ✕  connection refused",
		"│ │ 12:22:28  ✓  200 OK",
		"150ms │ │",
		"│ e/esc close ",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("error popup lacks %q:\n%s", want, all)
		}
	}
	press(m, "e")
	assertFrame(t, m.render(), 146, 36)
}

func TestPopupsFitSmallTerminal(t *testing.T) {
	f := newFixture(t, 30)
	m := f.model(80, 24)
	m.cursor = rowIndex(m, kubeID)
	press(m, "enter")
	assertFrame(t, m.render(), 80, 24)
	press(m, "esc")
	f.fail(7)
	press(m, "e")
	out := m.render()
	assertFrame(t, out, 80, 24)
	if !strings.Contains(plain(out), "e/esc close") {
		t.Error("error popup footer must stay visible at 80×24")
	}
}

func TestWrapText(t *testing.T) {
	got := wrapText("parse error: unexpected end of input at line 812 · scrape discarded", 20)
	for _, ln := range got {
		if format.Width(ln) > 20 {
			t.Errorf("line too wide: %q", ln)
		}
	}
	if strings.Join(got, " ") != "parse error: unexpected end of input at line 812 · scrape discarded" {
		t.Errorf("wrap lost words: %q", got)
	}
	if got := wrapText(strings.Repeat("x", 25), 10); len(got) != 3 || got[2] != "xxxxx" {
		t.Errorf("long word cut: %q", got)
	}
}

// A popup that is no longer drawn must not keep capturing keys.
func TestHiddenPopupsReleaseInput(t *testing.T) {
	f := newFixture(t, 30)
	f.fail(3)
	m := f.model(146, 36)
	m.cursor = 1 // the table is not scrolled
	press(m, "e")
	if !m.errOpen {
		t.Fatal("e opens the error popup while failing")
	}
	f.step() // the target recovers: the error popup is hidden
	press(m, "down")
	if m.cursor != 2 {
		t.Errorf("↓ after recovery must move the cursor: %d", m.cursor)
	}
	if !isQuit(press(m, "q")) {
		t.Error("q after recovery must quit, not close an invisible popup")
	}

	m = f.model(146, 36)
	press(m, "enter")
	if !m.detailOpen {
		t.Fatal("enter opens the details popup")
	}
	f.apply("") // no series left: the details popup is hidden
	if !isQuit(press(m, "q")) {
		t.Error("q with the details popup hidden must quit")
	}
}

var longLabels = `# HELP server_info Server information; the value is always 1.
# TYPE server_info gauge
server_info{id="2190f002-4e38-4a30-8aa2-0c5ae1f4d9b7",name="vm2",note="` + longNote + `",zone="pl1-a"} 1
server_info{name="vm3",path="` + widePath + `"} 1
`

var (
	longNote = strings.Repeat("abcdefghij", 20)                  // too long for any popup at 146 cells
	widePath = "/srv/" + strings.Repeat("0123456789", 11) + "/x" // fits once the popup widens
)

// labelLine returns the popup's labels-box line for key k, without borders.
func labelLine(pop []string, k string) string {
	for _, ln := range pop {
		if p := plain(ln); strings.HasPrefix(p, "│ │ "+k+" ") {
			return strings.TrimSuffix(strings.TrimRight(strings.TrimPrefix(p, "│ │ "), " │"), " ")
		}
	}
	return ""
}

func TestDetailsPopupLongLabels(t *testing.T) {
	f := newFixture(t, 0)
	f.apply(longLabels)
	f.apply(longLabels)
	m := f.model(146, 36)
	pick := func(name string) {
		for i, s := range m.rows() {
			if s.Labels[0].Value == name || len(s.Labels) > 1 && s.Labels[1].Value == name {
				m.setCursor(m.rows(), i)
				return
			}
		}
		t.Fatalf("no series with label %q", name)
	}
	popup := func() []string { return m.popup(layout{146, 30}, m.rows()) }

	pick("vm3") // widens to fit, nothing to scroll
	press(m, "enter")
	pop := popup()
	if w := format.Width(pop[0]); w != len(widePath)+16 { // "path" + 2, quoted value, 8 cells of borders
		t.Errorf("popup width = %d", w)
	}
	if got := labelLine(pop, "path"); got != `path  "`+widePath+`"` {
		t.Errorf("path line = %q", got)
	}
	if all := plain(strings.Join(pop, "\n")); strings.Contains(all, "◀") || strings.Contains(all, "▶") || strings.Contains(all, "scroll values") {
		t.Error("nothing to scroll sideways, yet the popup offers it")
	}

	press(m, "esc")
	pick("vm2") // as wide as the terminal allows, values scroll
	press(m, "enter")
	pop = popup()
	for i, ln := range pop {
		if format.Width(ln) != 142 {
			t.Fatalf("popup line %d is %d cells", i, format.Width(ln))
		}
	}
	all := plain(strings.Join(pop, "\n"))
	if !strings.Contains(all, "╭─ labels · 4 · ▶ ─") || !strings.Contains(all, "← → scroll values") {
		t.Errorf("no sideways scroll hints:\n%s", all)
	}
	vw := 134 - 6 // labels box inside minus the key column ("name" + 2)
	if got := labelLine(pop, "note"); got != `note  "`+longNote[:vw-2]+"…" {
		t.Errorf("note line = %q", got)
	}

	press(m, "right")
	if m.view != ViewTable || !m.detailOpen || m.labHOff != hStep {
		t.Fatalf("→ must scroll the values: view %v, open %v, off %d", m.view, m.detailOpen, m.labHOff)
	}
	pop = popup()
	if got := labelLine(pop, "note"); got != "note  …"+longNote[hStep:hStep+vw-2]+"…" {
		t.Errorf("scrolled note line = %q", got)
	}
	if got := labelLine(pop, "name"); got != `name  "vm2"` {
		t.Errorf("a short value must stay whole: %q", got)
	}
	if !strings.Contains(plain(pop[11]), "labels · 4 · ◀ ▶") {
		t.Errorf("title = %q", plain(pop[11]))
	}

	press(m, "end")
	pop = popup()
	if got := labelLine(pop, "note"); got != "note  …"+longNote[len(longNote)-vw+2:]+`"` {
		t.Errorf("note line at the end = %q", got)
	}
	if got := labelLine(pop, "id"); got != `id    "2190f002-4e38-4a30-8aa2-0c5ae1f4d9b7"` {
		t.Errorf("id line at the end = %q", got)
	}
	press(m, "right")
	if m.labHOff != len(longNote)+2-vw {
		t.Errorf("→ must stop at the end: %d", m.labHOff)
	}
	press(m, "home", "left")
	if m.labHOff != 0 {
		t.Errorf("home/← must stop at the start: %d", m.labHOff)
	}
	assertFrame(t, m.render(), 146, 36)

	press(m, "esc", "enter")
	if m.labHOff != 0 {
		t.Errorf("reopening must reset the sideways scroll: %d", m.labHOff)
	}
	press(m, "esc", "right")
	if m.view != ViewHistory {
		t.Errorf("with the popup closed → switches views: %v", m.view)
	}
}

func TestDetailsPopupShortLabelsTwoPerRow(t *testing.T) {
	f := newFixture(t, 30)
	m := f.model(146, 36)
	m.setCursor(m.rows(), slices.IndexFunc(m.rows(), func(s *store.Series) bool { return s.Name == "build_info" }))
	press(m, "enter")
	pop := m.popup(layout{146, 30}, m.rows())
	if w := format.Width(pop[0]); w != 106 {
		t.Errorf("popup width = %d", w)
	}
	if got := labelLine(pop, "version"); !strings.HasPrefix(got, `version    "2.14.3"`) || !strings.HasSuffix(got, `goversion  "go1.23.4"`) {
		t.Errorf("label line = %q", got)
	}
}

func TestSlide(t *testing.T) {
	for _, c := range []struct {
		s      string
		off, w int
		want   string
	}{
		{"abcdef", 0, 4, "abc…"},
		{"abcdef", 1, 4, "…cd…"},
		{"abcdef", 2, 4, "…def"},
		{"abc", 0, 4, "abc"},
	} {
		if got := slide(c.s, c.off, c.w); got != c.want {
			t.Errorf("slide(%q, %d, %d) = %q, want %q", c.s, c.off, c.w, got, c.want)
		}
	}
}
