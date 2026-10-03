package expo

import (
	"io"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

var typeWords = map[string]MetricType{
	"counter": Counter, "gauge": Gauge, "histogram": Histogram, "gaugehistogram": GaugeHistogram,
	"summary": Summary, "untyped": Untyped, "unknown": Untyped, "info": Gauge, "stateset": Gauge,
}

// suffixRules attach a sample to a declared family by name suffix.
var suffixRules = []struct {
	suffix string
	types  []MetricType
	drop   bool
}{
	{"_bucket", []MetricType{Histogram, GaugeHistogram}, false},
	{"_count", []MetricType{Histogram, Summary, GaugeHistogram}, false},
	{"_sum", []MetricType{Histogram, Summary, GaugeHistogram}, false},
	{"_gcount", []MetricType{Histogram, GaugeHistogram}, false},
	{"_gsum", []MetricType{Histogram, GaugeHistogram}, false},
	{"_total", []MetricType{Counter}, false},
	{"_info", []MetricType{Gauge}, false},
	{"_created", []MetricType{Counter, Histogram, Summary}, true},
}

type parser struct {
	byName   map[string]*Family
	declared map[string]bool
	order    []*Family
}

// Parse reads a whole payload. Any malformed line fails the parse with a
// *ParseError. match, when non-nil, keeps only families whose name matches.
func Parse(r io.Reader, match *regexp.Regexp) (*Parsed, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	text := strings.TrimSuffix(string(data), "\n")
	var lines []string
	if text != "" {
		lines = strings.Split(text, "\n")
	}
	// Raw retains lines after EOF even though they are not parsed as samples.
	// Sanitize the complete payload before parsing can stop at that marker.
	for i := range lines {
		lines[i] = Sanitize(strings.TrimSuffix(lines[i], "\r"))
	}
	p := &parser{byName: map[string]*Family{}, declared: map[string]bool{}}
	lineFam := make([]*Family, len(lines))
	for i := range lines {
		t := strings.TrimSpace(lines[i])
		if t == "" {
			continue
		}
		if t[0] == '#' {
			f, eof, err := p.comment(t, i+1)
			if err != nil {
				return nil, err
			}
			if eof {
				break
			}
			lineFam[i] = f
			continue
		}
		s, err := parseSample(t, i+1)
		if err != nil {
			return nil, err
		}
		s.Line = i
		f, keep := p.familyFor(s.Name)
		lineFam[i] = f
		if keep {
			f.Samples = append(f.Samples, s)
		}
	}
	return p.finish(lines, lineFam, match), nil
}

// Sanitize replaces terminal control characters (C0 except tab, DEL, C1)
// and invalid UTF-8 with U+FFFD, so scraped text (label values, HELP, Raw
// lines, HTTP status) cannot inject escape sequences. Tabs are data in label
// values and are kept; views escape them, as they escape a label value's \n,
// which is decoded afterwards and kept.
func Sanitize(s string) string {
	for i := 0; i < len(s); i++ {
		if c := s[i]; c < 0x20 && c != '\t' || c >= 0x7f {
			return strings.Map(func(r rune) rune {
				if r < 0x20 && r != '\t' || r >= 0x7f && r <= 0x9f {
					return utf8.RuneError
				}
				return r // invalid UTF-8 is written as U+FFFD by strings.Map
			}, s)
		}
	}
	return s
}

func (p *parser) comment(t string, line int) (*Family, bool, error) {
	kw, rest, _ := strings.Cut(strings.TrimSpace(t[1:]), " ")
	switch kw {
	case "EOF":
		return nil, true, nil
	case "HELP", "TYPE", "UNIT":
	default:
		return nil, false, nil // free-form comment
	}
	name, val, _ := strings.Cut(strings.TrimLeft(rest, " "), " ")
	if !validName(name) {
		return nil, false, &ParseError{line, "invalid metric name in # " + kw}
	}
	f := p.declare(name)
	switch kw {
	case "HELP":
		f.Help = unescapeHelp(val)
	case "UNIT":
		f.Unit = strings.TrimSpace(val)
	case "TYPE":
		word := strings.TrimSpace(val)
		typ, ok := typeWords[word]
		if !ok {
			return nil, false, &ParseError{line, "unknown metric type " + strconv.Quote(word)}
		}
		f.Type = typ
	}
	return f, false, nil
}

func (p *parser) declare(name string) *Family {
	p.declared[name] = true
	if f := p.byName[name]; f != nil {
		return f
	}
	f := &Family{Name: name}
	p.byName[name] = f
	p.order = append(p.order, f)
	return f
}

// familyFor finds the family a sample belongs to; keep=false drops it.
func (p *parser) familyFor(name string) (*Family, bool) {
	if p.declared[name] {
		return p.byName[name], true
	}
	for _, r := range suffixRules {
		base, ok := strings.CutSuffix(name, r.suffix)
		if !ok {
			continue
		}
		if f := p.byName[base]; f != nil && p.declared[base] && slices.Contains(r.types, f.Type) {
			if f.Type == Histogram && (r.suffix == "_gcount" || r.suffix == "_gsum") {
				f.Type = GaugeHistogram // the classic text format declares gauge histograms as histogram
			}
			return f, !r.drop
		}
		if r.suffix == "_created" {
			if f := p.byName[base+"_total"]; f != nil && p.declared[base+"_total"] && f.Type == Counter {
				return f, false
			}
		}
	}
	if f := p.byName[name]; f != nil {
		return f, true
	}
	f := &Family{Name: name}
	p.byName[name] = f
	p.order = append(p.order, f)
	return f, true
}

func (p *parser) finish(lines []string, lineFam []*Family, match *regexp.Regexp) *Parsed {
	keep := func(f *Family) bool { return match == nil || match.MatchString(f.Name) }
	out := &Parsed{}
	for _, f := range p.order {
		if len(f.Samples) > 0 && keep(f) {
			out.Families = append(out.Families, f)
			out.Samples += len(f.Samples)
		}
	}
	if match == nil {
		out.Lines = lines
		return out
	}
	remap := make([]int, len(lines))
	for i, ln := range lines {
		remap[i] = -1
		if f := lineFam[i]; f != nil && keep(f) {
			remap[i] = len(out.Lines)
			out.Lines = append(out.Lines, ln)
		}
	}
	for _, f := range out.Families {
		for j := range f.Samples {
			f.Samples[j].Line = remap[f.Samples[j].Line]
		}
	}
	return out
}

func parseSample(t string, line int) (Sample, error) {
	var s Sample
	i := 0
	for i < len(t) && isNameChar(t[i], i == 0) {
		i++
	}
	if i == 0 {
		return s, &ParseError{line, "invalid metric name"}
	}
	s.Name = t[:i]
	for i < len(t) && (t[i] == ' ' || t[i] == '\t') {
		i++
	}
	if i < len(t) && t[i] == '{' {
		ls, n, err := parseLabels(t[i:], line)
		if err != nil {
			return s, err
		}
		s.Labels, i = ls, i+n
	}
	fields := strings.Fields(t[i:])
	if len(fields) == 0 {
		return s, &ParseError{line, "unexpected end of input"}
	}
	v, err := parseValue(fields[0])
	if err != nil {
		return s, &ParseError{line, "invalid value " + strconv.Quote(fields[0])}
	}
	s.Value = v
	if len(fields) > 1 && fields[1] != "#" {
		if _, err := strconv.ParseFloat(fields[1], 64); err != nil {
			return s, &ParseError{line, "invalid timestamp " + strconv.Quote(fields[1])}
		}
		if len(fields) > 2 && fields[2] != "#" {
			return s, &ParseError{line, "unexpected text after timestamp"}
		}
	}
	return s, nil
}

// parseLabels parses "{…}" at the start of s and returns the bytes consumed.
func parseLabels(s string, line int) ([]Label, int, error) {
	var ls []Label
	i := 1
	skip := func() {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
	}
	for {
		skip()
		if i >= len(s) {
			return nil, 0, &ParseError{line, "unexpected end of input"}
		}
		if s[i] == '}' {
			return ls, i + 1, nil
		}
		st := i
		for i < len(s) && isLabelChar(s[i], i == st) {
			i++
		}
		if i == st {
			return nil, 0, &ParseError{line, "invalid label name"}
		}
		name := s[st:i]
		skip()
		if i >= len(s) {
			return nil, 0, &ParseError{line, "unexpected end of input"}
		}
		if s[i] != '=' {
			return nil, 0, &ParseError{line, "expected '=' after label " + name}
		}
		i++
		skip()
		if i >= len(s) {
			return nil, 0, &ParseError{line, "unexpected end of input"}
		}
		if s[i] != '"' {
			return nil, 0, &ParseError{line, "expected quoted value for label " + name}
		}
		i++
		var b strings.Builder
		closed := false
		for i < len(s) && !closed {
			c := s[i]
			switch {
			case c == '\\' && i+1 < len(s):
				switch s[i+1] {
				case 'n':
					b.WriteByte('\n')
				case '\\', '"':
					b.WriteByte(s[i+1])
				default:
					b.WriteByte('\\')
					b.WriteByte(s[i+1])
				}
				i += 2
			case c == '"':
				closed = true
				i++
			default:
				b.WriteByte(c)
				i++
			}
		}
		if !closed {
			return nil, 0, &ParseError{line, "unexpected end of input"}
		}
		ls = append(ls, Label{name, b.String()})
		skip()
		if i < len(s) && s[i] == ',' {
			i++
		}
	}
}

func parseValue(s string) (float64, error) {
	switch s {
	case "+Inf", "Inf":
		return math.Inf(1), nil
	case "-Inf":
		return math.Inf(-1), nil
	case "NaN":
		return math.NaN(), nil
	}
	return strconv.ParseFloat(s, 64)
}

func validName(s string) bool {
	for i := 0; i < len(s); i++ {
		if !isNameChar(s[i], i == 0) {
			return false
		}
	}
	return s != ""
}

func isNameChar(c byte, first bool) bool {
	return c == ':' || isLabelChar(c, first)
}

func isLabelChar(c byte, first bool) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || !first && c >= '0' && c <= '9'
}

func unescapeHelp(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+1 < len(s) {
			switch s[i+1] {
			case 'n':
				b.WriteByte('\n')
				i++
				continue
			case '\\', '"':
				b.WriteByte(s[i+1])
				i++
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
