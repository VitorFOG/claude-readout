package main

import (
	"math"
	"strings"
	"testing"
)

// Cases where Go's defaults differ from JavaScript's and the port has to side
// with the Node version.

func TestToFixed2RoundsExactTiesUp(t *testing.T) {
	for value, want := range map[float64]string{0.125: "0.13", 8.7: "8.70", 1.005: "1.00", 0: "0.00", 2.675: "2.67"} {
		if got := toFixed2(value); got != want {
			t.Errorf("toFixed2(%v) = %q, want %q", value, got, want)
		}
	}
}

func TestRampColorTreatsInfinityAsZero(t *testing.T) {
	stops := mustRamp(`["#000000","#ffffff"]`)
	if got := rampColor(math.Inf(1), stops); got != (RGB{0, 0, 0}) {
		t.Errorf("rampColor(+Inf) = %v, want black", got)
	}
}

func TestGaugePrintsNegativeZeroAsZero(t *testing.T) {
	percent := -0.3
	payload := Payload{}
	payload.RateLimits.FiveHour = &RateWindow{UsedPercentage: &percent}
	config := defaultConfig()
	config.Glyphs = GlyphSetting{Mode: "text"}
	out := renderHud(RenderInput{Payload: payload, Config: config})
	if strings.Contains(out, "-0%") || !strings.Contains(out, " 0%") {
		t.Errorf("negative zero leaked into %q", out)
	}
}

func TestJSRoundTakesHalvesUp(t *testing.T) {
	for value, want := range map[float64]float64{-0.5: 0, -1.5: -1, 0.5: 1, 2.5: 3, -0.3: 0, 1.4: 1} {
		if got := jsRound(value); got != want {
			t.Errorf("jsRound(%v) = %v, want %v", value, got, want)
		}
	}
}
