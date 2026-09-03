package main

import (
	"math"
	"regexp"
	"strconv"
	"strings"
)

// RGB is a colour as three 0..255 channels; intermediate ramp values are
// fractional, callers round when they print.
type RGB [3]float64

// Style paints text with SGR sequences, or leaves it alone when colour is off
// (NO_COLOR, --no-color, TERM=dumb). Every colour on the line goes through it.
type Style struct {
	Enabled bool
}

const ansiReset = "\x1b[0m"

// detectColorSupport honours NO_COLOR (any non-empty value), READOUT_NO_COLOR=1
// and TERM=dumb. Anything else is assumed to support truecolor.
func detectColorSupport(getenv func(string) string) bool {
	if getenv("NO_COLOR") != "" || getenv("READOUT_NO_COLOR") == "1" {
		return false
	}
	return getenv("TERM") != "dumb"
}

// parseHex parses "#rrggbb" or "#rgb" (leading # optional, surrounding space
// trimmed). Malformed input is a readable grey, #dddddd.
func parseHex(hex string) RGB {
	raw := strings.TrimSpace(hex)
	if strings.HasPrefix(raw, "#") {
		raw = raw[1:]
	}
	if len(raw) == 3 {
		raw = string([]byte{raw[0], raw[0], raw[1], raw[1], raw[2], raw[2]})
	}
	if len(raw) != 6 {
		return RGB{221, 221, 221}
	}
	var rgb RGB
	for i := range rgb {
		value, err := strconv.ParseUint(raw[i*2:i*2+2], 16, 8)
		if err != nil {
			return RGB{221, 221, 221}
		}
		rgb[i] = float64(value)
	}
	return rgb
}

// fg wraps text in a truecolor foreground from a hex string:
// "\x1b[38;2;r;g;bm<text>\x1b[0m".
func (s Style) fg(hex, text string) string {
	if !s.Enabled {
		return text
	}
	return s.fgRGB(parseHex(hex), text)
}

// fgRGB wraps text in a truecolor foreground, channels clamped and rounded.
func (s Style) fgRGB(rgb RGB, text string) string {
	if !s.Enabled {
		return text
	}
	channels := [3]string{}
	for i, value := range rgb {
		value = math.Round(value)
		if value < 0 {
			value = 0
		} else if value > 255 {
			value = 255
		}
		channels[i] = strconv.FormatFloat(value, 'f', -1, 64)
	}
	return "\x1b[38;2;" + strings.Join(channels[:], ";") + "m" + text + ansiReset
}

// dimFg opens dim and the colour in one sequence and closes once, so the dim
// attribute is not reset mid-string: "\x1b[2m\x1b[38;2;r;g;bm<text>\x1b[0m".
func (s Style) dimFg(hex, text string) string {
	if !s.Enabled {
		return text
	}
	rgb := parseHex(hex)
	return "\x1b[2m\x1b[38;2;" +
		strconv.Itoa(int(rgb[0])) + ";" + strconv.Itoa(int(rgb[1])) + ";" + strconv.Itoa(int(rgb[2])) +
		"m" + text + ansiReset
}

// dim is "\x1b[2m<text>\x1b[0m".
func (s Style) dim(text string) string {
	if !s.Enabled {
		return text
	}
	return "\x1b[2m" + text + ansiReset
}

// rampColor interpolates a percentage across the ramp. Percent is clamped to
// 0..100; NaN and infinities read as 0, as Number.isFinite made them in Node.
// Zero stops yield #dddddd; one stop is constant; below the first stop or above
// the last the end colour holds; between two stops with the same At the later
// one wins.
func rampColor(percent float64, stops Ramp) RGB {
	if math.IsNaN(percent) || math.IsInf(percent, 0) {
		percent = 0
	} else {
		percent = math.Max(0, math.Min(100, percent))
	}
	if len(stops) == 0 {
		return RGB{221, 221, 221}
	}
	if len(stops) == 1 || percent <= stops[0].At {
		return stops[0].RGB
	}
	last := stops[len(stops)-1]
	if percent >= last.At {
		return last.RGB
	}
	for i := 0; i < len(stops)-1; i++ {
		from, to := stops[i], stops[i+1]
		if percent < from.At || percent > to.At {
			continue
		}
		span := to.At - from.At
		if span == 0 {
			return to.RGB
		}
		t := (percent - from.At) / span
		return RGB{
			from.RGB[0] + (to.RGB[0]-from.RGB[0])*t,
			from.RGB[1] + (to.RGB[1]-from.RGB[1])*t,
			from.RGB[2] + (to.RGB[2]-from.RGB[2])*t,
		}
	}
	return last.RGB
}

// stripAnsi removes SGR sequences: "\x1b[" then digits and semicolons then "m".
func stripAnsi(text string) string {
	return sgrPattern.ReplaceAllString(text, "")
}

var sgrPattern = regexp.MustCompile("\\x1b\\[[0-9;]*m")
