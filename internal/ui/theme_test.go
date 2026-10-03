package ui

import (
	"image/color"
	"testing"
)

func rgb(c color.Color) color.RGBA { return color.RGBAModel.Convert(c).(color.RGBA) }

func TestOKLCH(t *testing.T) {
	if c := rgb(oklch(1, 0, 0)); c.R != 255 || c.G != 255 || c.B != 255 {
		t.Errorf("white = %v", c)
	}
	if c := rgb(oklch(0, 0, 0)); c.R != 0 || c.G != 0 || c.B != 0 {
		t.Errorf("black = %v", c)
	}
	if c := rgb(heatColor(1)); c.R <= c.G || c.G <= c.B {
		t.Errorf("hot end must be amber-ish: %v", c)
	}
	if c := rgb(heatColor(0.05)); c.B <= c.R {
		t.Errorf("cold end must be blue-ish: %v", c)
	}
}

func TestThemeLookups(t *testing.T) {
	th := newTheme(true)
	_ = th.heatStyle(0.0001)
	_ = th.heatStyle(1)
	if tag, _ := th.typeTag(0); tag != "unt" {
		t.Errorf("untyped tag = %q", tag)
	}
	_ = newTheme(false)
}
