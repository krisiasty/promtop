package store

import (
	"testing"
	"time"

	"github.com/krisiasty/promtop/internal/expo"
	"github.com/krisiasty/promtop/internal/scrape"
)

var t0 = time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)

func okResult(samples int, at time.Time) *scrape.Result {
	return &scrape.Result{Parsed: &expo.Parsed{Samples: samples}, Status: "200 OK", Duration: 5 * time.Millisecond, At: at}
}

func TestBackoffSequence(t *testing.T) {
	var st ScrapeState
	want := []time.Duration{1, 1, 1, 1, 2, 4, 8, 16, 30, 30}
	for i, w := range want {
		st.recordFail(&scrape.Error{At: t0}, time.Second)
		if got := st.Delay(time.Second); got != w*time.Second {
			t.Errorf("after %d failures delay = %v, want %v", i+1, got, w*time.Second)
		}
		if !st.NextTry.Equal(t0.Add(w * time.Second)) {
			t.Errorf("after %d failures NextTry = %v", i+1, st.NextTry)
		}
	}
	long := ScrapeState{Fails: 5}
	if got := long.Delay(10 * time.Second); got != 10*time.Second {
		t.Errorf("interval longer than backoff: %v", got)
	}
}

func TestStates(t *testing.T) {
	var st ScrapeState
	if st.State() != Connecting {
		t.Fatal("initial state must be Connecting")
	}
	st.recordFail(&scrape.Error{At: t0, Full: "x"}, time.Second)
	if st.State() != Degraded {
		t.Fatal("1 failure → Degraded")
	}
	for range 3 {
		st.recordFail(&scrape.Error{At: t0}, time.Second)
	}
	if st.State() != Degraded {
		t.Fatal("4 failures → still Degraded")
	}
	st.recordFail(&scrape.Error{At: t0}, time.Second)
	if st.State() != Down {
		t.Fatal("5 failures → Down")
	}
	st.recordOK(okResult(3, t0.Add(time.Second)), time.Second)
	if st.State() != Live || st.Fails != 0 || st.LastErr != nil || st.OKCount != 1 || st.Samples != 3 ||
		!st.LastOK.Equal(t0.Add(time.Second)) || !st.NextTry.Equal(t0.Add(2*time.Second)) {
		t.Fatalf("after success: %+v", st)
	}
	st.recordOK(okResult(0, t0.Add(2*time.Second)), time.Second)
	if st.State() != NoData {
		t.Fatal("empty payload → NoData")
	}
}

func TestAttemptsNewestFirstCapped(t *testing.T) {
	var st ScrapeState
	for i := range 12 {
		st.recordFail(&scrape.Error{At: t0.Add(time.Duration(i) * time.Second), Attempt: "connection refused"}, time.Second)
	}
	if len(st.Attempts) != MaxAttempts || !st.Attempts[0].At.Equal(t0.Add(11*time.Second)) || st.Attempts[0].Text != "connection refused" {
		t.Errorf("attempts = %+v", st.Attempts)
	}
	st.recordOK(okResult(1, t0.Add(12*time.Second)), time.Second)
	if a := st.Attempts[0]; !a.OK || a.Text != "200 OK" {
		t.Errorf("success attempt = %+v", a)
	}
}
