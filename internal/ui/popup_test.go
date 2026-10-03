package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/krisiasty/promtop/internal/format"
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
		{12, "│ │ cluster              \"prod-eu-1\" "},
		{19, "│ " + strings.Repeat("─", 102) + " │"},
		{20, "│ enter/esc close   space pin  ↑ ↓ scroll labels "},
	}
	for _, c := range checks {
		if !strings.HasPrefix(p[c.line], c.want) {
			t.Errorf("line %d = %q, want prefix %q", c.line, p[c.line], c.want)
		}
	}
	if !strings.HasSuffix(p[0], fmt.Sprintf(" %d / %d ─╮", idx+1, len(rows))) || !strings.HasSuffix(p[18], " ▼ 8 more ─╯ │") {
		t.Errorf("border markers: %q / %q", p[0], p[18])
	}
	press(m, "enter", "down", "down")
	pop = m.detailsPopup(layout{146, 30}, s, idx, len(rows))
	if top, bot := plain(pop[11]), plain(pop[18]); !strings.HasSuffix(top, " ▲ 4 more ─╮ │") || !strings.HasSuffix(bot, " ▼ 4 more ─╯ │") {
		t.Errorf("after scrolling: %q / %q", top, bot)
	}
	press(m, "down", "down", "down")
	if m.labOff != 4 {
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

// A popup that is no longer drawn must not keep capturing keys and clicks.
func TestHiddenPopupsReleaseInput(t *testing.T) {
	f := newFixture(t, 30)
	f.fail(3)
	m := f.model(146, 36)
	press(m, "e")
	if !m.errOpen {
		t.Fatal("e opens the error popup while failing")
	}
	f.step() // the target recovers: the error popup is hidden
	press(m, "down")
	if m.cursor != 2 {
		t.Errorf("↓ after recovery must move the cursor: %d", m.cursor)
	}
	click(m, 10, 3+1+4)
	if m.cursor != 4 {
		t.Errorf("row click after recovery → cursor %d", m.cursor)
	}
	wheel(m, true)
	if m.cursor != 5 {
		t.Errorf("wheel after recovery → cursor %d", m.cursor)
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
