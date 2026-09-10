package main

import (
	"math"
	"strconv"
	"testing"
)

func TestGlyphTablesShareKeys(t *testing.T) {
	if len(nerdGlyphs) != len(glyphKeys) || len(textGlyphs) != len(glyphKeys) || len(elementMeanings) != len(glyphKeys) {
		t.Fatalf("table sizes: keys %d nerd %d text %d meanings %d", len(glyphKeys), len(nerdGlyphs), len(textGlyphs), len(elementMeanings))
	}
	for _, key := range glyphKeys {
		if _, ok := nerdGlyphs[key]; !ok {
			t.Errorf("%s missing from nerdGlyphs", key)
		}
		if _, ok := textGlyphs[key]; !ok {
			t.Errorf("%s missing from textGlyphs", key)
		}
		if _, ok := elementMeanings[key]; !ok {
			t.Errorf("%s missing from elementMeanings", key)
		}
	}
}

func TestThemePalettesReadableOnTheirBackground(t *testing.T) {
	backgrounds := map[string]string{"dark": "#1a1b26", "light": "#ffffff"}
	for name, paletteFn := range themes {
		palette := paletteFn()
		background := parseHex(backgrounds[name])
		colors := map[string]struct {
			rgb     RGB
			minimum float64
		}{
			"text":     {parseHex(palette.Text), 3.0},
			"accent":   {parseHex(palette.Accent), 3.0},
			"scoped":   {parseHex(palette.Scoped), 3.0},
			"ok":       {parseHex(palette.OK), 3.0},
			"warn":     {parseHex(palette.Warn), 3.0},
			"crit":     {parseHex(palette.Crit), 3.0},
			"muted":    {parseHex(palette.Muted), 2.5},
			"barEmpty": {parseHex(palette.BarEmpty), 1.2},
		}
		for i, stop := range palette.Bar {
			colors["bar stop "+strconv.Itoa(i)] = struct {
				rgb     RGB
				minimum float64
			}{stop.RGB, 3.0}
		}
		for colorName, check := range colors {
			ratio := contrastRatio(check.rgb, background)
			if ratio < check.minimum {
				t.Errorf("%s %s %v contrast ratio %.2f, want at least %.1f", name, colorName, check.rgb, ratio, check.minimum)
			}
		}
	}
}

func contrastRatio(a, b RGB) float64 {
	lightA := relativeLuminance(a)
	lightB := relativeLuminance(b)
	return (math.Max(lightA, lightB) + 0.05) / (math.Min(lightA, lightB) + 0.05)
}

func relativeLuminance(rgb RGB) float64 {
	linear := func(channel float64) float64 {
		channel /= 255
		if channel <= 0.04045 {
			return channel / 12.92
		}
		return math.Pow((channel+0.055)/1.055, 2.4)
	}
	return 0.2126*linear(rgb[0]) + 0.7152*linear(rgb[1]) + 0.0722*linear(rgb[2])
}
