// Package expo parses the Prometheus text exposition format (0.0.4) and
// OpenMetrics text (1.0) into metric families and samples.
package expo

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
)

// MetricType is a family's declared type. OpenMetrics types map onto these:
// info and stateset → Gauge, unknown → Untyped. GaugeHistogram stays distinct,
// and a histogram exposing _gcount or _gsum (classic text format) becomes one.
type MetricType uint8

const (
	Untyped MetricType = iota
	Counter
	Gauge
	Histogram
	Summary
	GaugeHistogram
)

func (t MetricType) String() string {
	switch t {
	case Counter:
		return "counter"
	case Gauge:
		return "gauge"
	case Histogram:
		return "histogram"
	case GaugeHistogram:
		return "gaugehistogram"
	case Summary:
		return "summary"
	}
	return "untyped"
}

// IsHistogram reports whether the type describes a bucket distribution.
func (t MetricType) IsHistogram() bool { return t == Histogram || t == GaugeHistogram }

// Label is one name="value" pair, kept in exposition order.
type Label struct{ Name, Value string }

// Sample is one exposition line's value.
type Sample struct {
	Name   string
	Labels []Label
	Value  float64
	Line   int // 0-based index into Parsed.Lines
}

// Family groups the samples of one metric (including histogram/summary
// _bucket, _sum and _count samples).
type Family struct {
	Name, Help, Unit string
	Type             MetricType
	Samples          []Sample
}

// Parsed is one scrape's payload.
type Parsed struct {
	Families []*Family // exposition order, only families with samples
	Lines    []string  // raw payload lines, for the Raw view
	Samples  int
}

// ParseError reports the first malformed line; it discards the whole scrape.
type ParseError struct {
	Line int // 1-based
	Msg  string
}

func (e *ParseError) Error() string { return fmt.Sprintf("parse error: %s at line %d", e.Msg, e.Line) }

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

// LabelString renders labels as {k="v",…} in the given order ("" if none).
func LabelString(ls []Label) string {
	if len(ls) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteByte('{')
	for i, l := range ls {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(l.Name)
		b.WriteString(`="`)
		_, _ = labelEscaper.WriteString(&b, l.Value) // strings.Builder never fails
		b.WriteByte('"')
	}
	b.WriteByte('}')
	return b.String()
}

// ID is a series' display identity: name plus labels in exposition order.
func ID(name string, ls []Label) string { return name + LabelString(ls) }

// Key identifies a series regardless of label order. It leaves ls unchanged
// so callers can still render labels in exposition order.
func Key(name string, ls []Label) string {
	if len(ls) > 1 && !slices.IsSortedFunc(ls, compareLabels) {
		ls = slices.Clone(ls)
		slices.SortFunc(ls, compareLabels)
	}
	return name + LabelString(ls)
}

func compareLabels(a, b Label) int {
	if n := cmp.Compare(a.Name, b.Name); n != 0 {
		return n
	}
	return cmp.Compare(a.Value, b.Value)
}
