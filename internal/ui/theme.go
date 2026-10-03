package ui

import (
	"image/color"
	"math"

	"charm.land/lipgloss/v2"

	"github.com/krisiasty/promtop/internal/expo"
)

type palette struct {
	bg, line, dim, mid, fg, hi, sel  color.Color
	cyan, green, amber, red, magenta color.Color
	prev, stat, axis, gutter         color.Color
	amberBg, redBg, cyanBg           color.Color
}

// darkPalette holds the design tokens (README "Design tokens").
var darkPalette = palette{
	bg: lipgloss.Color("#111317"), line: lipgloss.Color("#2b3038"), dim: lipgloss.Color("#7b8494"),
	mid: lipgloss.Color("#aab1bd"), fg: lipgloss.Color("#d6dae1"), hi: lipgloss.Color("#eef0f3"),
	sel: lipgloss.Color("#1d2430"), cyan: lipgloss.Color("#6cc9dc"), green: lipgloss.Color("#86d49a"),
	amber: lipgloss.Color("#e8bf6a"), red: lipgloss.Color("#f07b73"), magenta: lipgloss.Color("#c79ae6"),
	prev: lipgloss.Color("#8e97a6"), stat: lipgloss.Color("#b3bac5"), axis: lipgloss.Color("#3d434d"),
	gutter: lipgloss.Color("#4a515c"), amberBg: lipgloss.Color("#2a2413"), redBg: lipgloss.Color("#2a1719"),
	cyanBg: lipgloss.Color("#14242b"),
}

// lightPalette is a hand-picked counterpart for light terminals; the design
// itself is dark-only.
var lightPalette = palette{
	bg: lipgloss.Color("#ffffff"), line: lipgloss.Color("#d0d4da"), dim: lipgloss.Color("#6b7280"),
	mid: lipgloss.Color("#4b5563"), fg: lipgloss.Color("#1f2328"), hi: lipgloss.Color("#0b0d10"),
	sel: lipgloss.Color("#e3e8f0"), cyan: lipgloss.Color("#0e7490"), green: lipgloss.Color("#15803d"),
	amber: lipgloss.Color("#a16207"), red: lipgloss.Color("#b91c1c"), magenta: lipgloss.Color("#7e22ce"),
	prev: lipgloss.Color("#5b6472"), stat: lipgloss.Color("#3f4652"), axis: lipgloss.Color("#b8bec7"),
	gutter: lipgloss.Color("#9aa1ab"), amberBg: lipgloss.Color("#fdf3dc"), redBg: lipgloss.Color("#fde2e0"),
	cyanBg: lipgloss.Color("#dff3f8"),
}

const heatSteps = 64

type theme struct {
	dark                                       bool
	c                                          palette
	line, dim, mid, fg, hi, hiBold             lipgloss.Style
	cyan, cyanBold, green, amber, red, magenta lipgloss.Style
	prev, stat, axis, gutter                   lipgloss.Style
	badge, tabActive, changed                  lipgloss.Style
	heat                                       [heatSteps]lipgloss.Style
}

func newTheme(dark bool) theme {
	c := lightPalette
	if dark {
		c = darkPalette
	}
	fg := func(col color.Color) lipgloss.Style { return lipgloss.NewStyle().Foreground(col) }
	t := theme{
		dark: dark, c: c, line: fg(c.line), dim: fg(c.dim), mid: fg(c.mid), fg: fg(c.fg), hi: fg(c.hi), hiBold: fg(c.hi).Bold(true),
		cyan: fg(c.cyan), cyanBold: fg(c.cyan).Bold(true), green: fg(c.green), amber: fg(c.amber), red: fg(c.red),
		magenta: fg(c.magenta), prev: fg(c.prev), stat: fg(c.stat), axis: fg(c.axis), gutter: fg(c.gutter),
		badge:     lipgloss.NewStyle().Foreground(c.bg).Background(c.cyan).Bold(true),
		tabActive: lipgloss.NewStyle().Foreground(c.bg).Background(c.cyan),
		changed:   lipgloss.NewStyle().Foreground(c.amber).Background(c.amberBg),
	}
	for i := range t.heat {
		col := heatColor(float64(i+1) / heatSteps)
		t.heat[i] = lipgloss.NewStyle().Foreground(col).Background(col)
	}
	return t
}

// heatStyle is the heatmap cell style for intensity f in (0,1]: glyph and
// background share the ramp colour, so cells look solid on screen while the
// glyph survives in plain-text renders.
func (t *theme) heatStyle(f float64) lipgloss.Style {
	return t.heat[min(heatSteps-1, max(0, int(math.Ceil(f*heatSteps))-1))]
}

// typeTag is the Table/Series type column text and colour.
func (t *theme) typeTag(typ expo.MetricType) (string, lipgloss.Style) {
	switch typ {
	case expo.Counter:
		return "ctr", t.magenta
	case expo.Gauge:
		return "gau", t.green
	case expo.Histogram:
		return "his", t.amber
	case expo.GaugeHistogram:
		return "ghs", t.green
	case expo.Summary:
		return "sum", t.amber
	}
	return "unt", t.dim
}

// heatColor is the design's blue→amber ramp: oklch(0.30+0.55f, 0.05+0.10f, 260−190f).
func heatColor(f float64) color.Color { return oklch(0.30+0.55*f, 0.05+0.10*f, 260-190*f) }

// oklch converts an OKLCH colour to sRGB (Björn Ottosson's matrices).
func oklch(l, c, hDeg float64) color.Color {
	h := hDeg * math.Pi / 180
	a, b := c*math.Cos(h), c*math.Sin(h)
	l1 := l + 0.3963377774*a + 0.2158037573*b
	m1 := l - 0.1055613458*a - 0.0638541728*b
	s1 := l - 0.0894841775*a - 1.2914855480*b
	L, M, S := l1*l1*l1, m1*m1*m1, s1*s1*s1
	return color.RGBA{
		R: srgb(4.0767416621*L - 3.3077115913*M + 0.2309699292*S),
		G: srgb(-1.2684380046*L + 2.6097574011*M - 0.3413193965*S),
		B: srgb(-0.0041960863*L - 0.7034186147*M + 1.7076147010*S),
		A: 255,
	}
}

func srgb(x float64) uint8 {
	if x <= 0.0031308 {
		x *= 12.92
	} else {
		x = 1.055*math.Pow(x, 1/2.4) - 0.055
	}
	return uint8(math.Round(math.Max(0, math.Min(1, x)) * 255))
}
