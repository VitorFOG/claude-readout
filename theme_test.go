package main

import "testing"

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
