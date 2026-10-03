package expo

import (
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

const promText = `# HELP up 1 if the target is reachable, 0 otherwise.
# TYPE up gauge
up 1
# HELP http_requests_total Total HTTP requests processed, by method and status code.
# TYPE http_requests_total counter
http_requests_total{method="GET",code="200"} 12798715
http_requests_total{method="POST",code="500"} 53334 1690000000000
# HELP http_request_duration_seconds HTTP request latency in seconds.
# TYPE http_request_duration_seconds histogram
http_request_duration_seconds_bucket{le="0.1"} 10
http_request_duration_seconds_bucket{le="+Inf"} 12
http_request_duration_seconds_sum 1.5
http_request_duration_seconds_count 12
# HELP go_gc_duration_seconds A summary of the pause duration of GC cycles.
# TYPE go_gc_duration_seconds summary
go_gc_duration_seconds{quantile="0.5"} 0.00052
go_gc_duration_seconds_sum 3.1
go_gc_duration_seconds_count 4821
weird_untyped_metric NaN
temperature_celsius{room="a\"b\\c\nd"} -Inf
`

func mustParse(t *testing.T, text string, match *regexp.Regexp) *Parsed {
	t.Helper()
	p, err := Parse(strings.NewReader(text), match)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return p
}

func TestParseFamilies(t *testing.T) {
	p := mustParse(t, promText, nil)
	var names []string
	for _, f := range p.Families {
		names = append(names, f.Name+":"+f.Type.String())
	}
	want := "up:gauge http_requests_total:counter http_request_duration_seconds:histogram go_gc_duration_seconds:summary weird_untyped_metric:untyped temperature_celsius:untyped"
	if got := strings.Join(names, " "); got != want {
		t.Fatalf("families = %q\nwant       %q", got, want)
	}
	if p.Samples != 12 {
		t.Errorf("Samples = %d, want 12", p.Samples)
	}
	if len(p.Lines) != 20 {
		t.Errorf("Lines = %d, want 20", len(p.Lines))
	}
	hist := p.Families[2]
	if len(hist.Samples) != 4 || hist.Samples[1].Labels[0].Value != "+Inf" {
		t.Errorf("histogram samples = %+v", hist.Samples)
	}
	req := p.Families[1].Samples[0]
	if req.Value != 12798715 || req.Line != 5 || ID(req.Name, req.Labels) != `http_requests_total{method="GET",code="200"}` {
		t.Errorf("sample = %+v", req)
	}
	if p.Families[1].Help != "Total HTTP requests processed, by method and status code." {
		t.Errorf("help = %q", p.Families[1].Help)
	}
	if !math.IsNaN(p.Families[4].Samples[0].Value) {
		t.Error("NaN not parsed")
	}
	temp := p.Families[5].Samples[0]
	if temp.Labels[0].Value != "a\"b\\c\nd" || !math.IsInf(temp.Value, -1) {
		t.Errorf("escapes/-Inf = %+v", temp)
	}
	if got := LabelString(temp.Labels); got != `{room="a\"b\\c\nd"}` {
		t.Errorf("LabelString = %s", got)
	}
}

func TestKeyIgnoresLabelOrderWithoutChangingDisplay(t *testing.T) {
	labels := []Label{{Name: "b", Value: "2"}, {Name: "a", Value: "1"}}
	if got, want := Key("m", labels), `m{a="1",b="2"}`; got != want {
		t.Errorf("Key = %q, want %q", got, want)
	}
	if got, want := ID("m", labels), `m{b="2",a="1"}`; got != want {
		t.Errorf("display ID changed: %q, want %q", got, want)
	}
	if got := Key("m", []Label{{Name: "a", Value: "1"}, {Name: "b", Value: "2"}}); got != Key("m", labels) {
		t.Errorf("reordered labels have different keys: %q", got)
	}
	if got := Key("m", []Label{{Name: "a", Value: "2"}, {Name: "b", Value: "2"}}); got == Key("m", labels) {
		t.Error("different label values have the same key")
	}
	// Duplicate names are not valid Prometheus labels, but sorting values as
	// well makes the key independent of their order if they appear in input.
	if a, b := Key("m", []Label{{"x", "2"}, {"x", "1"}}), Key("m", []Label{{"x", "1"}, {"x", "2"}}); a != b {
		t.Errorf("duplicate-name label order changed the key: %q vs %q", a, b)
	}
}

func TestParseOpenMetrics(t *testing.T) {
	text := "# TYPE foo counter\n# UNIT foo seconds\n# HELP foo Foo \\\"quoted\\\" help.\n" +
		"foo_total 3 # {trace_id=\"x\"} 1\nfoo_created 1.6e9\n" +
		"# TYPE build info\nbuild_info{version=\"1\"} 1\n# EOF\n"
	p := mustParse(t, text, nil)
	if len(p.Families) != 2 {
		t.Fatalf("families = %d, want 2", len(p.Families))
	}
	foo := p.Families[0]
	if foo.Type != Counter || foo.Unit != "seconds" || len(foo.Samples) != 1 ||
		foo.Samples[0].Name != "foo_total" || foo.Help != `Foo "quoted" help.` {
		t.Errorf("foo = %+v", foo)
	}
	if b := p.Families[1]; b.Type != Gauge || b.Samples[0].Name != "build_info" {
		t.Errorf("info family = %+v", b)
	}
}

func TestParseGaugeHistogramTypeAndTotals(t *testing.T) {
	p := mustParse(t, "# TYPE h gaugehistogram\nh_bucket{le=\"1\"} 10\nh_bucket{le=\"+Inf\"} 20\nh_gsum 15\nh_gcount 20\n# EOF\n", regexp.MustCompile("^h$"))
	if len(p.Families) != 1 || p.Samples != 4 {
		t.Fatalf("gauge histogram grouping = %+v", p)
	}
	f := p.Families[0]
	if f.Type.String() != "gaugehistogram" || f.Samples[2].Name != "h_gsum" || f.Samples[3].Name != "h_gcount" {
		t.Errorf("gauge histogram type and totals = %+v", f)
	}
}

func TestParseGaugeHistogramTotalSpellings(t *testing.T) {
	for _, tc := range []struct{ name, text string }{
		// client_python's classic text format declares gauge histograms as histogram.
		{"classic text", "# TYPE h histogram\nh_bucket{le=\"1\"} 10\nh_bucket{le=\"+Inf\"} 20\nh_gsum 15\nh_gcount 20\n"},
		{"plain totals", "# TYPE h gaugehistogram\nh_bucket{le=\"1\"} 10\nh_bucket{le=\"+Inf\"} 20\nh_sum 15\nh_count 20\n# EOF\n"},
	} {
		p := mustParse(t, tc.text, nil)
		if len(p.Families) != 1 || p.Samples != 4 || p.Families[0].Type != GaugeHistogram {
			t.Errorf("%s: families = %+v; want one gaugehistogram with 4 samples", tc.name, p.Families)
		}
	}
}

func TestParseSanitizesLinesAfterEOF(t *testing.T) {
	text := "# TYPE up gauge\nup 1\n# EOF\n" +
		"# \x1b]8;;https://example.org/\x1b\\click\x1b]8;;\x1b\\\u009b2J\r\n" +
		"up 2\n# invalid \xff\n"
	for _, tc := range []struct {
		name    string
		match   *regexp.Regexp
		samples int
		lines   int
	}{
		{"unfiltered", nil, 1, 6},
		{"matching", regexp.MustCompile("^up$"), 1, 2},
		{"excluded", regexp.MustCompile("^missing$"), 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := mustParse(t, text, tc.match)
			if p.Samples != tc.samples || len(p.Families) != tc.samples || len(p.Lines) != tc.lines {
				t.Fatalf("samples/families/lines = %d/%d/%d, want %d/%d/%d",
					p.Samples, len(p.Families), len(p.Lines), tc.samples, tc.samples, tc.lines)
			}
			if tc.samples > 0 {
				s := p.Families[0].Samples[0]
				if s.Value != 1 || p.Lines[s.Line] != "up 1" {
					t.Errorf("EOF must stop sampling and preserve raw line mapping: %+v", s)
				}
			}
			if tc.match == nil {
				want := "# �]8;;https://example.org/�\\click�]8;;�\\�2J"
				if p.Lines[3] != want || p.Lines[4] != "up 2" || p.Lines[5] != "# invalid �" {
					t.Errorf("raw lines after EOF = %q, want sanitized controls and invalid UTF-8", p.Lines[3:])
				}
			}
		})
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		text string
		line int
		msg  string
	}{
		{"up 1\nhttp_requests_total{method=\"GE", 2, "unexpected end of input"},
		{"up abc", 1, `invalid value "abc"`},
		{"up", 1, "unexpected end of input"},
		{"# TYPE up bogus", 1, `unknown metric type "bogus"`},
		{"up{1a=\"x\"} 1", 1, "invalid label name"},
		{"up 1 notatime", 1, `invalid timestamp "notatime"`},
		{"<html><body>hi</body></html>", 1, "invalid metric name"},
	}
	for _, c := range cases {
		_, err := Parse(strings.NewReader(c.text), nil)
		var pe *ParseError
		if !errors.As(err, &pe) || pe.Line != c.line || pe.Msg != c.msg {
			t.Errorf("%q: err = %v, want line %d %q", c.text, err, c.line, c.msg)
			continue
		}
		if want := "parse error: " + c.msg + " at line " + strconv.Itoa(c.line); err.Error() != want {
			t.Errorf("Error() = %q, want %q", err.Error(), want)
		}
	}
}

func TestParseMatch(t *testing.T) {
	p := mustParse(t, promText, regexp.MustCompile(`^(?:http_.*)$`))
	if len(p.Families) != 2 || p.Samples != 6 || len(p.Lines) != 10 {
		t.Fatalf("families=%d samples=%d lines=%d, want 2/6/10", len(p.Families), p.Samples, len(p.Lines))
	}
	s := p.Families[0].Samples[0]
	if p.Lines[s.Line] != `http_requests_total{method="GET",code="200"} 12798715` {
		t.Errorf("remapped line = %q", p.Lines[s.Line])
	}
}

func TestParseEmpty(t *testing.T) {
	p := mustParse(t, "", nil)
	if len(p.Families) != 0 || p.Samples != 0 || len(p.Lines) != 0 {
		t.Errorf("empty payload = %+v", p)
	}
}

// Terminal control characters in scraped text (C0 except tab, DEL, C1 and
// invalid UTF-8) must never reach the screen: they become U+FFFD.
func TestParseSanitizesControlCharacters(t *testing.T) {
	text := "# HELP evil bad\x1b]0;pwn\x07 help\u009b2J\n" +
		"# TYPE evil gauge\n" +
		"evil{v=\"a\x1b[2Jb\u0085c\x7fd\x9be\\nf\",w=\"x\ty\"} 1\n" +
		"# free comment \x1b[31mred\r\n"
	p := mustParse(t, text, nil)
	f := p.Families[0]
	if want := "bad�]0;pwn� help�2J"; f.Help != want {
		t.Errorf("HELP = %q, want %q", f.Help, want)
	}
	s := f.Samples[0]
	if want := "a�[2Jb�c�d�e\nf"; s.Labels[0].Value != want { // the \n escape still decodes
		t.Errorf("label value = %q, want %q", s.Labels[0].Value, want)
	}
	if s.Labels[1].Value != "x\ty" || s.Value != 1 {
		t.Errorf("tab and value must be untouched: %q %v", s.Labels[1].Value, s.Value)
	}
	for i, ln := range p.Lines {
		for _, r := range ln {
			if r < 0x20 && r != '\t' || r >= 0x7f && r <= 0x9f {
				t.Errorf("line %d keeps control %U: %q", i, r, ln)
			}
		}
	}
	if want := "# free comment �[31mred"; p.Lines[3] != want {
		t.Errorf("comment line = %q, want %q", p.Lines[3], want)
	}
}

func TestParseBlanksBeforeLabels(t *testing.T) {
	p := mustParse(t, "# TYPE a counter\na_total {code=\"200\"} 5\na_total\t{code=\"500\"} 1\n", nil)
	ss := p.Families[0].Samples
	if len(ss) != 2 || ss[0].Value != 5 || ss[1].Value != 1 ||
		LabelString(ss[0].Labels) != `{code="200"}` || LabelString(ss[1].Labels) != `{code="500"}` {
		t.Errorf("samples = %+v", ss)
	}
}
