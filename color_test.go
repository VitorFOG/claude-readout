package main

import (
	"math"
	"reflect"
	"testing"
)

func TestColorParseHex(t *testing.T) {
	tests := []struct {
		input string
		want  RGB
	}{
		{"#fff", RGB{255, 255, 255}},
		{"7aa2f7", RGB{122, 162, 247}},
		{"nope", RGB{221, 221, 221}},
	}
	for _, tt := range tests {
		if got := parseHex(tt.input); got != tt.want {
			t.Errorf("parseHex(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestColorRamp(t *testing.T) {
	flat := mustRamp(`[
		{"at":0,"color":"#9ece6a"},
		{"at":55,"color":"#9ece6a"},
		{"at":100,"color":"#f7768e"}
	]`)
	for _, tt := range []struct {
		percent float64
		want    RGB
	}{
		{0, RGB{158, 206, 106}},
		{40, RGB{158, 206, 106}},
		{100, RGB{247, 118, 142}},
	} {
		if got := rampColor(tt.percent, flat); got != tt.want {
			t.Errorf("rampColor(%v, flat) = %v, want %v", tt.percent, got, tt.want)
		}
	}

	even := mustRamp(`["#000000", "#ffffff"]`)
	mid := rampColor(50, even)
	wantMid := RGB{128, 128, 128}
	for i := range mid {
		mid[i] = math.Round(mid[i])
	}
	if mid != wantMid {
		t.Errorf("rounded midpoint = %v, want %v", mid, wantMid)
	}
	for _, tt := range []struct {
		percent float64
		want    RGB
	}{
		{-20, RGB{0, 0, 0}},
		{500, RGB{255, 255, 255}},
		{math.NaN(), RGB{0, 0, 0}},
	} {
		if got := rampColor(tt.percent, even); got != tt.want {
			t.Errorf("rampColor(%v, even) = %v, want %v", tt.percent, got, tt.want)
		}
	}
}

func TestColorStyleAndDetection(t *testing.T) {
	env := map[string]string{}
	getenv := func(key string) string { return env[key] }
	if !detectColorSupport(getenv) {
		t.Fatal("color unexpectedly disabled")
	}
	for key, value := range map[string]string{"NO_COLOR": "yes", "READOUT_NO_COLOR": "1", "TERM": "dumb"} {
		env = map[string]string{key: value}
		if detectColorSupport(getenv) {
			t.Errorf("color enabled with %s=%q", key, value)
		}
	}
	env = map[string]string{"NO_COLOR": "", "READOUT_NO_COLOR": "0", "TERM": "xterm"}
	if !detectColorSupport(getenv) {
		t.Fatal("empty NO_COLOR should not disable color")
	}

	on := Style{Enabled: true}
	if got, want := on.fg("#7aa2f7", "x"), "\x1b[38;2;122;162;247mx\x1b[0m"; got != want {
		t.Errorf("fg = %q, want %q", got, want)
	}
	if got, want := on.fgRGB(RGB{-1, 127.5, 300}, "x"), "\x1b[38;2;0;128;255mx\x1b[0m"; got != want {
		t.Errorf("fgRGB = %q, want %q", got, want)
	}
	if got, want := on.dimFg("#fff", "x"), "\x1b[2m\x1b[38;2;255;255;255mx\x1b[0m"; got != want {
		t.Errorf("dimFg = %q, want %q", got, want)
	}
	if got := stripAnsi("\x1b[32mok\x1b[0m"); got != "ok" {
		t.Errorf("stripAnsi = %q, want ok", got)
	}
	off := Style{}
	got := []string{off.fg("#fff", "x"), off.fgRGB(RGB{}, "x"), off.dimFg("#fff", "x"), off.dim("x")}
	if !reflect.DeepEqual(got, []string{"x", "x", "x", "x"}) {
		t.Errorf("disabled painters = %v", got)
	}
}
