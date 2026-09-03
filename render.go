package main

import (
	"fmt"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	barFilled = "█"
	barEmpty  = "░"
)

// RenderInput is everything renderHud needs. Nothing in it does I/O.
type RenderInput struct {
	Payload Payload
	Config  Config
	Git     *GitInfo // nil outside a work tree or when the git line is off
	Scoped  []Bucket // model-scoped weekly buckets from the usage cache
	// ToolCalls is nil when the transcript is unavailable.
	ToolCalls *int
	// Columns is the terminal width when known, else 0.
	Columns int
	Style   Style
	// Now is the clock reset times count down from; tests pin it.
	Now time.Time
}

// formatDuration renders milliseconds as 45s, 20m, 1h5m, 3d2h (days and hours
// only above a day, hours and minutes below, whole seconds under a minute).
// Negative or NaN is not a duration.
func formatDuration(ms float64) (string, bool) {
	if math.IsNaN(ms) || math.IsInf(ms, 0) || ms < 0 {
		return "", false
	}
	totalMinutes := int64(math.Floor(ms / 60_000))
	if totalMinutes < 1 {
		return strconv.FormatInt(int64(math.Floor(ms/1000)), 10) + "s", true
	}
	days := totalMinutes / 1440
	hours := totalMinutes % 1440 / 60
	minutes := totalMinutes % 60
	if days > 0 {
		result := strconv.FormatInt(days, 10) + "d"
		if hours > 0 {
			result += strconv.FormatInt(hours, 10) + "h"
		}
		return result, true
	}
	if hours > 0 {
		result := strconv.FormatInt(hours, 10) + "h"
		if minutes > 0 {
			result += strconv.FormatInt(minutes, 10) + "m"
		}
		return result, true
	}
	return strconv.FormatInt(minutes, 10) + "m", true
}

// formatUntil is the duration from now until a timestamp, or false when the
// timestamp is unknown or already past.
func formatUntil(when Timestamp, now time.Time) (string, bool) {
	if when.IsZero() {
		return "", false
	}
	diff := when.Time.Sub(now)
	if diff <= 0 {
		return "", false
	}
	return formatDuration(float64(diff) / float64(time.Millisecond))
}

// visibleWidth counts terminal cells: SGR sequences stripped, ZWJ and combining
// marks zero, CJK and emoji ranges two, else one.
func visibleWidth(text string) int {
	width := 0
	for _, r := range stripAnsi(text) {
		if r == 0x200d || r >= 0x0300 && r <= 0x036f {
			continue
		}
		wide := r >= 0x1100 && r <= 0x115f ||
			r >= 0x2e80 && r <= 0xa4cf ||
			r >= 0xac00 && r <= 0xd7a3 ||
			r >= 0xf900 && r <= 0xfaff ||
			r >= 0xfe30 && r <= 0xfe6f ||
			r >= 0xff00 && r <= 0xff60 ||
			r >= 0x1f300 && r <= 0x1f9ff
		if wide {
			width += 2
		} else {
			width++
		}
	}
	return width
}

// renderHud composes the statusline: an optional git line, then the meter line,
// joined with "\n" and without a trailing newline. It reproduces the Node
// version at 4a7f521: element descriptors, progressive compaction levels,
// shedding in shed order, wrap and clip.
func renderHud(in RenderInput) string {
	glyphs := resolveGlyphs(in.Config.Glyphs)
	palette := in.Config.Palette
	separator := glyphs["separator"]
	if in.Config.Separator != nil {
		separator = *in.Config.Separator
	}
	separator = " " + in.Style.dim(separator) + " "

	type compactionLevel struct {
		showResets bool
		bar        int
		context    int
	}
	type descriptor struct {
		key    string
		render func(compactionLevel) string
	}

	labelFor := func(name string, ambiguous bool) string {
		switch in.Config.Labels {
		case "never":
			return ""
		case "always":
			return name
		default:
			if ambiguous {
				return name
			}
			return ""
		}
	}
	meter := func(percent float64, width int) string {
		if width <= 0 {
			return ""
		}
		safe := percent
		if math.IsNaN(safe) || math.IsInf(safe, 0) {
			safe = 0
		} else {
			safe = math.Max(0, math.Min(100, safe))
		}
		filled := int(jsRound(safe / 100 * float64(width)))
		if safe > 0 && filled < 1 {
			filled = 1
		}
		return in.Style.fgRGB(rampColor(safe, palette.Bar), strings.Repeat(barFilled, filled)) +
			in.Style.fg(palette.BarEmpty, strings.Repeat(barEmpty, width-filled))
	}
	gauge := func(glyph, label string, percent *float64, resetsAt Timestamp, width int, tint string, showReset bool) string {
		if percent == nil {
			return ""
		}
		value := jsRound(*percent)
		color := palette.Muted
		if tint != "" {
			color = tint
		}
		parts := make([]string, 0, 5)
		if glyph != "" {
			parts = append(parts, in.Style.fg(color, glyph))
		}
		if label != "" && label != glyph {
			parts = append(parts, in.Style.fg(color, label))
		}
		if bar := meter(value, width); bar != "" {
			parts = append(parts, bar)
		}
		parts = append(parts, in.Style.fgRGB(rampColor(value, palette.Bar), strconv.Itoa(int(value))+"%"))
		if showReset {
			if until, ok := formatUntil(resetsAt, in.Now); ok {
				parts = append(parts, in.Style.dimFg(palette.Muted, "("+until+")"))
			}
		}
		return strings.Join(parts, " ")
	}

	descriptors := make([]descriptor, 0, 9+len(in.Scoped))
	if in.Config.element("model") {
		descriptors = append(descriptors, descriptor{key: "model", render: func(compactionLevel) string {
			name := in.Payload.Model.DisplayName
			if name == "" {
				name = in.Payload.Model.ID
			}
			if name == "" {
				return ""
			}
			short := strings.TrimSpace(modelSuffixPattern.ReplaceAllString(name, ""))
			if short == "" {
				short = name
			}
			large := in.Payload.ContextWindow.ContextWindowSize >= 1_000_000
			result := in.Style.fg(palette.Muted, glyphs["model"]) + " " + in.Style.fg(palette.Accent, short)
			if large {
				result += in.Style.dim(" 1M")
			}
			return result
		}})
	}
	if in.Config.element("fiveHour") {
		descriptors = append(descriptors, descriptor{key: "fiveHour", render: func(level compactionLevel) string {
			if in.Payload.RateLimits.FiveHour == nil {
				return ""
			}
			window := in.Payload.RateLimits.FiveHour
			return gauge(glyphs["fiveHour"], labelFor(in.Config.name("fiveHour"), true), window.UsedPercentage, window.ResetsAt, level.bar, "", level.showResets)
		}})
	}
	if in.Config.element("weekly") {
		descriptors = append(descriptors, descriptor{key: "weekly", render: func(level compactionLevel) string {
			if in.Payload.RateLimits.SevenDay == nil {
				return ""
			}
			window := in.Payload.RateLimits.SevenDay
			return gauge(glyphs["weekly"], labelFor(in.Config.name("weekly"), true), window.UsedPercentage, window.ResetsAt, level.bar, "", level.showResets)
		}})
	}
	if in.Config.element("scoped") {
		for _, bucket := range in.Scoped {
			bucket := bucket
			descriptors = append(descriptors, descriptor{key: "scoped", render: func(level compactionLevel) string {
				percent := float64(bucket.Percent)
				return gauge(glyphs["scoped"], bucket.Label, &percent, bucket.ResetsAt, level.bar, palette.Scoped, level.showResets)
			}})
		}
	}
	if in.Config.element("context") && in.Payload.ContextWindow.UsedPercentage != nil {
		descriptors = append(descriptors, descriptor{key: "context", render: func(level compactionLevel) string {
			return gauge(glyphs["context"], labelFor(in.Config.name("context"), false), in.Payload.ContextWindow.UsedPercentage, Timestamp{}, level.context, "", true)
		}})
	}
	if in.Config.element("thinking") {
		descriptors = append(descriptors, descriptor{key: "thinking", render: func(compactionLevel) string {
			if !in.Payload.Thinking.Enabled {
				return ""
			}
			result := in.Style.fg(palette.Muted, glyphs["thinking"])
			if in.Payload.Effort.Level != "" {
				result += in.Style.dim(" " + in.Payload.Effort.Level)
			}
			return result
		}})
	}
	if in.Config.element("session") {
		descriptors = append(descriptors, descriptor{key: "session", render: func(compactionLevel) string {
			if in.Payload.Cost.TotalDurationMs == nil {
				return ""
			}
			duration, ok := formatDuration(*in.Payload.Cost.TotalDurationMs)
			if !ok {
				return ""
			}
			return in.Style.fg(palette.Muted, glyphs["session"]) + " " + in.Style.fg(palette.Text, duration)
		}})
	}
	if in.Config.element("tools") {
		descriptors = append(descriptors, descriptor{key: "tools", render: func(compactionLevel) string {
			if in.ToolCalls == nil || *in.ToolCalls <= 0 {
				return ""
			}
			return in.Style.fg(palette.Muted, glyphs["tools"]) + " " + in.Style.fg(palette.Text, strconv.Itoa(*in.ToolCalls))
		}})
	}
	if in.Config.element("cost") {
		descriptors = append(descriptors, descriptor{key: "cost", render: func(compactionLevel) string {
			if in.Payload.Cost.TotalCostUSD == nil || math.IsNaN(*in.Payload.Cost.TotalCostUSD) || math.IsInf(*in.Payload.Cost.TotalCostUSD, 0) {
				return ""
			}
			cost := "$" + toFixed2(*in.Payload.Cost.TotalCostUSD)
			return in.Style.fg(palette.Muted, glyphs["cost"]) + " " + in.Style.fg(palette.Text, cost)
		}})
	}

	barWidth := in.Config.BarWidth
	contextWidth := in.Config.ContextBarWidth
	levels := []compactionLevel{
		{showResets: true, bar: barWidth, context: contextWidth},
		{bar: barWidth, context: contextWidth},
		{bar: min(barWidth, 5), context: min(contextWidth, 6)},
		{bar: 3, context: 3},
		{},
	}
	compose := func(level compactionLevel, keep []bool) []string {
		parts := make([]string, 0, len(descriptors))
		for i, item := range descriptors {
			if keep != nil && !keep[i] {
				continue
			}
			if part := item.render(level); part != "" {
				parts = append(parts, part)
			}
		}
		return parts
	}

	width := 0
	widthKnown := in.Columns > 20
	if widthKnown {
		width = max(20, in.Columns-in.Config.ReserveColumns)
	}
	parts := compose(levels[0], nil)
	if widthKnown && in.Config.Overflow != "none" {
		found := false
		for _, level := range levels {
			candidate := compose(level, nil)
			if visibleWidth(strings.Join(candidate, separator)) <= width {
				parts = candidate
				found = true
				break
			}
		}
		if !found {
			tightest := levels[len(levels)-1]
			if in.Config.Overflow == "wrap" {
				parts = compose(tightest, nil)
			} else {
				keep := make([]bool, len(descriptors))
				for i := range keep {
					keep[i] = true
				}
				shedOrder := in.Config.ShedOrder
				if shedOrder == nil {
					shedOrder = defaultShedOrder
				}
				for _, key := range shedOrder {
					if visibleWidth(strings.Join(compose(tightest, keep), separator)) <= width {
						break
					}
					for i := len(descriptors) - 1; i >= 0; i-- {
						if keep[i] && descriptors[i].key == key {
							keep[i] = false
							break
						}
					}
				}
				parts = compose(tightest, keep)
			}
		}
	}

	lines := make([]string, 0, 2)
	if in.Config.ShowGitLine {
		gitParts := renderGitParts(in, glyphs, palette)
		if len(gitParts) > 0 {
			line := strings.Join(gitParts, separator)
			if widthKnown {
				line = clipRendered(line, width)
			}
			lines = append(lines, line)
		}
	}
	if widthKnown && in.Config.Overflow == "wrap" {
		lines = append(lines, wrapRenderedParts(parts, separator, width)...)
	} else {
		lines = append(lines, strings.Join(parts, separator))
	}
	return strings.Join(lines, "\n")
}

var modelSuffixPattern = regexp.MustCompile(`\s*\(.*\)\s*$`)

// jsRound is JavaScript's Math.round: halves go toward positive infinity, so
// -0.5 is 0 where Go's math.Round gives -1.
func jsRound(value float64) float64 {
	floor := math.Floor(value)
	if value-floor >= 0.5 {
		return floor + 1
	}
	return floor
}

// toFixed2 is JavaScript's Number.prototype.toFixed(2), decided on the exact
// binary value: 0.125 is a true tie and rounds up, 2.675 is just under one and
// rounds down. Go's %.2f rounds the tie to even, and scaling by 100 first
// rounds 2.675 up, so neither matches the Node version.
func toFixed2(value float64) string {
	sign := ""
	if value < 0 {
		sign = "-"
		value = -value
	}
	scaled := new(big.Float).SetPrec(128).SetFloat64(value)
	scaled.Mul(scaled, big.NewFloat(100)).Add(scaled, big.NewFloat(0.5))
	hundredths, _ := scaled.Int(nil)
	whole, cents := new(big.Int).DivMod(hundredths, big.NewInt(100), new(big.Int))
	return fmt.Sprintf("%s%s.%02d", sign, whole, cents)
}

func renderGitParts(in RenderInput, glyphs GlyphSet, palette Palette) []string {
	if in.Git == nil {
		return nil
	}
	parts := make([]string, 0, 3)
	if in.Config.element("repo") && in.Git.Repo != "" {
		parts = append(parts, in.Style.fg(palette.Muted, glyphs["repo"])+" "+in.Style.fg(palette.Text, in.Git.Repo))
	}
	if in.Config.element("branch") && in.Git.Branch != "" {
		parts = append(parts, in.Style.fg(palette.Muted, glyphs["branch"])+" "+in.Style.fg(palette.Accent, in.Git.Branch))
	}
	if in.Config.element("gitStatus") {
		status := make([]string, 0, 5)
		if in.Git.Staged > 0 {
			status = append(status, in.Style.fg(palette.OK, glyphs["dirty"]+strconv.Itoa(in.Git.Staged)))
		}
		if in.Git.Modified > 0 {
			status = append(status, in.Style.fg(palette.Warn, glyphs["dirty"]+strconv.Itoa(in.Git.Modified)))
		}
		if in.Git.Untracked > 0 {
			status = append(status, in.Style.fg(palette.Muted, glyphs["untracked"]+strconv.Itoa(in.Git.Untracked)))
		}
		if in.Git.Ahead > 0 {
			status = append(status, in.Style.fg(palette.OK, glyphs["ahead"]+strconv.Itoa(in.Git.Ahead)))
		}
		if in.Git.Behind > 0 {
			status = append(status, in.Style.fg(palette.Crit, glyphs["behind"]+strconv.Itoa(in.Git.Behind)))
		}
		if len(status) > 0 {
			parts = append(parts, strings.Join(status, " "))
		}
	}
	return parts
}

func wrapRenderedParts(parts []string, separator string, width int) []string {
	lines := make([]string, 0, len(parts))
	current := make([]string, 0, len(parts))
	for _, part := range parts {
		candidate := append(append([]string(nil), current...), part)
		if len(current) > 0 && visibleWidth(strings.Join(candidate, separator)) > width {
			lines = append(lines, strings.Join(current, separator))
			current = []string{part}
		} else {
			current = candidate
		}
	}
	if len(current) > 0 {
		lines = append(lines, strings.Join(current, separator))
	}
	return lines
}

// clipRendered cuts the text to width terminal cells, ellipsis included,
// counting cells the way visibleWidth does so a wide rune cannot push the line
// past the pane.
func clipRendered(text string, width int) string {
	if visibleWidth(text) <= width {
		return text
	}
	limit := max(0, width-1)
	kept := make([]rune, 0, limit)
	cells := 0
	for _, r := range stripAnsi(text) {
		w := visibleWidth(string(r))
		if cells+w > limit {
			break
		}
		kept = append(kept, r)
		cells += w
	}
	return string(kept) + "…"
}
