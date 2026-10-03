// Package format renders numbers, durations and small text graphics exactly
// as the promtop design specifies.
package format

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// Unit selects how Num renders a value.
type Unit uint8

const (
	None      Unit = iota // SI suffixes (k, M, G)
	Bytes                 // binary units (Ki, Mi, Gi)
	Seconds               // milliseconds below one second
	Timestamp             // Unix epoch seconds; rendered as a time by Value
)

// Minus is U+2212, the sign the design uses for negative deltas.
const Minus = "−"

// Num formats v per the design: SI suffixes with three significant digits for
// plain numbers, binary units for bytes, milliseconds below one second for
// seconds. NaN renders as "—".
func Num(v float64, u Unit) string {
	switch {
	case math.IsNaN(v):
		return "—"
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	}
	a := math.Abs(v)
	switch u {
	case Bytes:
		switch {
		case a >= 1<<30:
			return fixed(v/(1<<30), 2) + "Gi"
		case a >= 1<<20:
			return fixed(v/(1<<20), 1) + "Mi"
		case a >= 1<<10:
			return fixed(v/(1<<10), 1) + "Ki"
		}
		return fixed(v, 0) + "B"
	case Seconds, Timestamp: // Timestamp without a reference time: plain seconds
		switch {
		case a == 0:
			return "0"
		case a < 1e-6:
			return fixed(v*1e9, 1) + "ns"
		case a < 1e-4:
			return fixed(v*1e6, 2) + "µs"
		case a < 0.01:
			return fixed(v*1000, 2) + "ms"
		case a < 1:
			return fixed(v*1000, 1) + "ms"
		}
		return fixed(v, 2) + "s"
	}
	return SI(v)
}

// SI formats a plain number with three significant digits and k/M/G
// suffixes; values below 0.01 get four decimals ("0.0004"), and values that
// round to zero at that precision render as "0".
func SI(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return Num(v, None) // "—", "+Inf", "-Inf"
	}
	a := math.Abs(v)
	switch {
	case a == 0:
		return "0"
	case a >= 1e9:
		return fixed(v/1e9, 2) + "G"
	case a >= 1e6:
		return fixed(v/1e6, 2) + "M"
	case a >= 1e4:
		return fixed(v/1e3, 1) + "k"
	case a >= 100:
		return fixed(v, 0)
	case a >= 10:
		return fixed(v, 1)
	case a >= 1:
		return fixed(v, 2)
	case a >= 0.01:
		return fixed(v, 3)
	case a < 0.00005:
		return "0"
	}
	return fixed(v, 4)
}

// Signed formats a delta with an explicit sign: "+7.00", "−355.2Ki", "0".
// A delta too small to show at the unit's precision renders as "0".
func Signed(d float64, u Unit) string {
	switch {
	case math.IsNaN(d):
		return "—"
	case d == 0 || Num(math.Abs(d), u) == "0":
		return "0"
	case d > 0:
		return "+" + Num(d, u)
	}
	return Minus + Num(-d, u)
}

// Int formats n with comma thousands separators: 20083 → "20,083".
func Int(n int) string {
	s := strconv.Itoa(n)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// Size formats a byte count with decimal units: "3.9 MB".
func Size(n int) string {
	f := float64(n)
	switch {
	case f >= 1e9:
		return fixed(f/1e9, 1) + " GB"
	case f >= 1e6:
		return fixed(f/1e6, 1) + " MB"
	case f >= 1e3:
		return fixed(f/1e3, 1) + " kB"
	}
	return strconv.Itoa(n) + " B"
}

// Raw formats an exposition value as the Raw view shows it: integers
// verbatim, anything else rounded to at most four decimals.
func Raw(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return strconv.FormatFloat(v, 'f', -1, 64)
	}
	if v == math.Trunc(v) && math.Abs(v) < 1e15 {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(math.Round(v*1e4)/1e4, 'f', -1, 64)
}

// RawDelta prefixes positive deltas with "+"; negatives keep an ASCII "-".
func RawDelta(d float64) string {
	if d > 0 {
		return "+" + Raw(d)
	}
	return Raw(d)
}

// Dur renders a duration rounded to whole seconds: "18s", "5m", "3m45s", "2h5m".
func Dur(d time.Duration) string {
	s := max(0, int(math.Round(d.Seconds())))
	if s < 60 {
		return strconv.Itoa(s) + "s"
	}
	if s < 3600 {
		m, r := s/60, s%60
		if r == 0 {
			return strconv.Itoa(m) + "m"
		}
		return strconv.Itoa(m) + "m" + strconv.Itoa(r) + "s"
	}
	if s < 86400 {
		h, m := s/3600, (s%3600)/60
		if m == 0 {
			return strconv.Itoa(h) + "h"
		}
		return strconv.Itoa(h) + "h" + strconv.Itoa(m) + "m"
	}
	days, h := s/86400, (s%86400)/3600
	if h == 0 {
		return strconv.Itoa(days) + "d"
	}
	return strconv.Itoa(days) + "d" + strconv.Itoa(h) + "h"
}

// Clock renders t as HH:MM:SS in local time.
func Clock(t time.Time) string { return t.Local().Format("15:04:05") }

// Interval renders a configured duration compactly: "1s", "500ms", "1m30s", "2m".
func Interval(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// Millis renders a scrape duration: "150ms" below one second, "1.23s" above.
func Millis(d time.Duration) string {
	if d < time.Second {
		return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
	}
	return fixed(d.Seconds(), 2) + "s"
}

func fixed(v float64, prec int) string { return strconv.FormatFloat(v, 'f', prec, 64) }

// IsEpoch reports whether v looks like Unix epoch seconds (≥ 1e9, finite).
func IsEpoch(v float64) bool { return v >= 1e9 && !math.IsInf(v, 1) }

// Value formats v like Num, except that epoch-like Timestamp values render as
// a compact local time relative to now: "15:04:05" on the same day, "Jan 02"
// in the same year, "Jan 2006" otherwise.
func Value(v float64, u Unit, now time.Time) string {
	if u != Timestamp || !IsEpoch(v) {
		return Num(v, u)
	}
	t, n := epoch(v).Local(), now.Local()
	switch {
	case t.Year() == n.Year() && t.YearDay() == n.YearDay():
		return t.Format("15:04:05")
	case t.Year() == n.Year():
		return t.Format("Jan 02")
	}
	return t.Format("Jan 2006")
}

// Delta formats a difference like Signed; Timestamp deltas render as signed
// durations ("+3s", "−3h5m").
func Delta(d float64, u Unit) string {
	if u != Timestamp || math.IsNaN(d) {
		return Signed(d, u)
	}
	dur := time.Duration(math.Abs(d) * float64(time.Second))
	switch {
	case math.Round(math.Abs(d)) == 0:
		return "0"
	case d > 0:
		return "+" + Dur(dur)
	}
	return Minus + Dur(dur)
}

// TimeFull renders a timestamp as local date and time plus its age:
// "2026-09-21 13:33:20 · 4d23h ago" or "… · in 12d".
func TimeFull(v float64, now time.Time) string {
	t := epoch(v)
	age := Dur(now.Sub(t)) + " ago"
	if t.After(now) {
		age = "in " + Dur(t.Sub(now))
	}
	return t.Local().Format("2006-01-02 15:04:05") + " · " + age
}

func epoch(v float64) time.Time {
	sec, frac := math.Modf(v)
	return time.Unix(int64(sec), int64(frac*1e9))
}
