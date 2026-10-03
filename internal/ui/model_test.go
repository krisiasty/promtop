package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/scrape"
)

func okResult(t *testing.T) *scrape.Result {
	p, err := expo.Parse(strings.NewReader("up 1\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	return &scrape.Result{Parsed: p, Status: "200 OK", At: fixtureBase}
}

func TestNoOverlappingScrapes(t *testing.T) {
	calls := 0
	m := New(testConfig(), Options{Now: func() time.Time { return fixtureBase },
		Scrape: func(context.Context) (*scrape.Result, error) { calls++; return okResult(t), nil }})
	cmd := m.scrapeCmd()
	if cmd == nil {
		t.Fatal("first scrape must start")
	}
	if m.scrapeCmd() != nil {
		t.Fatal("a second scrape must wait for the first")
	}
	if _, c := m.Update(tickMsg{}); c != nil {
		t.Fatal("a tick during an in-flight scrape must not start another")
	}
	if _, next := m.Update(cmd()); next == nil || m.inFlight || m.st.Seq() != 1 || calls != 1 {
		t.Fatalf("after the result: next=%v inFlight=%v seq=%d calls=%d", next != nil, m.inFlight, m.st.Seq(), calls)
	}
}

func TestPauseStopsScraping(t *testing.T) {
	m := New(testConfig(), Options{Now: func() time.Time { return fixtureBase },
		Scrape: func(context.Context) (*scrape.Result, error) { return okResult(t), nil }})
	press(m, "p")
	if !m.paused {
		t.Fatal("p pauses")
	}
	if _, c := m.Update(tickMsg{}); c != nil {
		t.Error("no scrape while paused")
	}
	m.Update(scrapeOKMsg{okResult(t)})
	if m.st.Seq() != 0 {
		t.Error("results arriving while paused are dropped")
	}
	if c := press(m, "p"); c == nil {
		t.Error("resume scrapes immediately")
	}
}

func TestScrapeErrorsReachTheStore(t *testing.T) {
	m := New(testConfig(), Options{Now: func() time.Time { return fixtureBase },
		Scrape: func(context.Context) (*scrape.Result, error) {
			return nil, &scrape.Error{Kind: scrape.KindRefused, Full: refusedFull, Short: "connection refused", At: fixtureBase}
		}})
	m.Update(m.scrapeCmd()())
	if m.st.Scrape.Fails != 1 || m.st.Scrape.LastErr.Short != "connection refused" {
		t.Errorf("scrape state = %+v", m.st.Scrape)
	}
}

// A tick scheduled before a pause must not start a second scrape loop when it
// fires after the resume (the resume already restarted the chain).
func TestPauseResumeKeepsOneScrapeLoop(t *testing.T) {
	calls := 0
	m := New(testConfig(), Options{Now: func() time.Time { return fixtureBase.Add(time.Hour) }, // NextTry is past: ticks fire at once
		Scrape: func(context.Context) (*scrape.Result, error) { calls++; return okResult(t), nil }})
	_, tick := m.Update(m.scrapeCmd()())
	if tick == nil {
		t.Fatal("a result schedules the next tick")
	}
	stale := tick()
	press(m, "p")
	resume := press(m, "p")
	if resume == nil {
		t.Fatal("resume scrapes immediately")
	}
	_, next := m.Update(resume())
	if next == nil || calls != 2 {
		t.Fatalf("after the resumed scrape: next=%v calls=%d", next != nil, calls)
	}
	if _, c := m.Update(stale); c != nil {
		t.Fatal("a tick from before the pause started a second scrape loop")
	}
	if _, c := m.Update(next()); c == nil {
		t.Error("the current loop's tick must still scrape")
	}
}
