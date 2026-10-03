package store

import (
	"time"

	"github.com/krisiasty/promtop/internal/scrape"
)

// State is the endpoint's health as shown in the header.
type State uint8

const (
	Connecting State = iota // nothing succeeded or failed yet
	Live
	NoData   // last successful scrape had zero samples
	Degraded // 1–4 consecutive failures
	Down     // 5 or more consecutive failures
)

const (
	DownAfter   = 5
	MaxAttempts = 10
	MaxBackoff  = 30 * time.Second
)

// Attempt is one entry of the "last 10 attempts" list.
type Attempt struct {
	At       time.Time
	OK       bool
	Text     string // "200 OK" or the error's attempt text
	Duration time.Duration
}

// ScrapeState tracks scrape outcomes and the retry schedule.
type ScrapeState struct {
	Fails       int
	LastOK      time.Time
	LastErr     *scrape.Error
	Attempts    []Attempt // newest first, at most MaxAttempts
	NextTry     time.Time
	Duration    time.Duration // of the last successful scrape
	Samples     int
	Bytes       int
	Status      string
	ContentType string
	OKCount     uint64
}

// State derives the header state from the counters.
func (st *ScrapeState) State() State {
	switch {
	case st.Fails >= DownAfter:
		return Down
	case st.Fails > 0:
		return Degraded
	case st.OKCount == 0:
		return Connecting
	case st.Samples == 0:
		return NoData
	}
	return Live
}

// Delay is the wait before the next attempt: the interval for the first four
// failures, then 2s → 4s → 8s … capped at 30s, never below the interval.
func (st *ScrapeState) Delay(interval time.Duration) time.Duration {
	if st.Fails < DownAfter {
		return interval
	}
	d := MaxBackoff
	if n := st.Fails - (DownAfter - 1); n < 5 {
		d = time.Duration(1<<n) * time.Second
	}
	return max(min(d, MaxBackoff), interval)
}

func (st *ScrapeState) recordOK(r *scrape.Result, interval time.Duration) {
	st.Fails, st.LastErr, st.LastOK = 0, nil, r.At
	st.Duration, st.Bytes, st.Status, st.ContentType = r.Duration, r.Bytes, r.Status, r.ContentType
	st.Samples = 0
	if r.Parsed != nil {
		st.Samples = r.Parsed.Samples
	}
	st.OKCount++
	st.push(Attempt{At: r.At, OK: true, Text: r.Status, Duration: r.Duration})
	st.NextTry = r.At.Add(st.Delay(interval))
}

func (st *ScrapeState) recordFail(e *scrape.Error, interval time.Duration) {
	st.Fails++
	st.LastErr = e
	st.push(Attempt{At: e.At, Text: e.Attempt, Duration: e.Duration})
	st.NextTry = e.At.Add(st.Delay(interval))
}

func (st *ScrapeState) push(a Attempt) {
	st.Attempts = append([]Attempt{a}, st.Attempts...)
	if len(st.Attempts) > MaxAttempts {
		st.Attempts = st.Attempts[:MaxAttempts]
	}
}
