package main

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

func floatPointer(value float64) *float64 { return &value }

func textRenderFixture() (Payload, Config, time.Time) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	var payload Payload
	payload.Model.ID = "claude-opus-5[1m]"
	payload.Model.DisplayName = "Opus 5 (1M context)"
	payload.ContextWindow.UsedPercentage = floatPointer(23)
	payload.ContextWindow.ContextWindowSize = 1_000_000
	payload.Cost.TotalDurationMs = floatPointer(29 * 60_000)
	payload.Cost.TotalCostUSD = floatPointer(8.7)
	payload.Thinking.Enabled = true
	payload.Effort.Level = "xhigh"
	payload.RateLimits.FiveHour = &RateWindow{
		UsedPercentage: floatPointer(38),
		ResetsAt:       Timestamp{Time: now.Add(time.Hour)},
	}
	payload.RateLimits.SevenDay = &RateWindow{
		UsedPercentage: floatPointer(14),
		ResetsAt:       Timestamp{Time: now.Add(24 * time.Hour)},
	}
	config := defaultConfig()
	config.Glyphs = GlyphSetting{Mode: "text"}
	separator := "|"
	config.Separator = &separator
	return payload, config, now
}

func TestFormatDuration(t *testing.T) {
	for _, tt := range []struct {
		ms   float64
		want string
		ok   bool
	}{
		{30_000, "30s", true},
		{20 * 60_000, "20m", true},
		{65 * 60_000, "1h5m", true},
		{50 * 3_600_000, "2d2h", true},
		{-1, "", false},
	} {
		got, ok := formatDuration(tt.ms)
		if got != tt.want || ok != tt.ok {
			t.Errorf("formatDuration(%v) = %q, %v; want %q, %v", tt.ms, got, ok, tt.want, tt.ok)
		}
	}
}

func TestFormatUntilAndVisibleWidth(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	got, ok := formatUntil(Timestamp{Time: now.Add(2 * time.Hour)}, now)
	if !ok || got != "2h" {
		t.Errorf("formatUntil future = %q, %v", got, ok)
	}
	for _, when := range []Timestamp{{Time: now.Add(-time.Minute)}, {}} {
		if got, ok := formatUntil(when, now); ok || got != "" {
			t.Errorf("formatUntil(%v) = %q, %v", when.Time, got, ok)
		}
	}
	for _, tt := range []struct {
		text string
		want int
	}{
		{"\x1b[32mok\x1b[0m", 2},
		{"🔧", 2},
		{"e\u0301", 1},
		{"a\u200db", 2},
	} {
		if got := visibleWidth(tt.text); got != tt.want {
			t.Errorf("visibleWidth(%q) = %d, want %d", tt.text, got, tt.want)
		}
	}
}

func TestRenderGlyphlessMeter(t *testing.T) {
	_, config, now := textRenderFixture()
	config.Glyphs = GlyphSetting{Overrides: map[string]string{"scoped": ""}}
	out := renderHud(RenderInput{
		Config: config,
		Scoped: []Bucket{{ID: "fable", Label: "Fable", Percent: 22}},
		Style:  Style{},
		Now:    now,
	})
	if out != "Fable ██░░░░░░ 22%" {
		t.Errorf("renderHud = %q", out)
	}
}

func TestRenderLabels(t *testing.T) {
	payload, config, now := textRenderFixture()
	config.Labels = "always"
	out := renderHud(RenderInput{Payload: payload, Config: config, Style: Style{}, Now: now})
	if regexp.MustCompile(`5h\s+5h`).MatchString(out) {
		t.Errorf("text glyph repeated label: %q", out)
	}
	if !regexp.MustCompile(`5h .*38%`).MatchString(out) || !regexp.MustCompile(`ctx .*23%`).MatchString(out) {
		t.Errorf("expected always labels in %q", out)
	}

	config.Labels = "auto"
	config.Glyphs = GlyphSetting{Overrides: map[string]string{"fiveHour": "@", "weekly": "@", "scoped": "@", "context": "@"}}
	out = renderHud(RenderInput{
		Payload: payload,
		Config:  config,
		Scoped:  []Bucket{{ID: "fable", Label: "Fable", Percent: 22}},
		Style:   Style{},
		Now:     now,
	})
	for _, pattern := range []string{`@ 5h .*38%`, `@ wk .*14%`, `@ Fable .*22%`, `@ .*23%`} {
		if !regexp.MustCompile(pattern).MatchString(out) {
			t.Errorf("%q did not match %q", out, pattern)
		}
	}
	if strings.Contains(out, "ctx") {
		t.Errorf("auto context unexpectedly labelled: %q", out)
	}

	config.Labels = "never"
	config.Glyphs = GlyphSetting{Overrides: map[string]string{"fiveHour": "@", "weekly": "%", "scoped": "*"}}
	out = renderHud(RenderInput{
		Payload: payload,
		Config:  config,
		Scoped:  []Bucket{{ID: "fable", Label: "Fable", Percent: 22}},
		Style:   Style{},
		Now:     now,
	})
	if !regexp.MustCompile(`\* Fable .*22%`).MatchString(out) || strings.Contains(out, "5h") || strings.Contains(out, "wk") {
		t.Errorf("never labels rendered incorrectly: %q", out)
	}
}

func TestRenderEveryElementAndGit(t *testing.T) {
	payload, config, now := textRenderFixture()
	toolCalls := 108
	out := renderHud(RenderInput{
		Payload:   payload,
		Config:    config,
		Scoped:    []Bucket{{ID: "fable", Label: "Fable", Percent: 22}},
		ToolCalls: &toolCalls,
		Style:     Style{},
		Now:       now,
	})
	for _, pattern := range []string{`model Opus 5`, `5h .*38%`, `wk .*14%`, `Fable .*22%`, `ctx .*23%`, `think xhigh`, `up 29m`, `tools 108`} {
		if !regexp.MustCompile(pattern).MatchString(out) {
			t.Errorf("%q did not match %q", out, pattern)
		}
	}
	if strings.Contains(out, "\n") {
		t.Errorf("render without git contains newline: %q", out)
	}

	out = renderHud(RenderInput{
		Payload: payload,
		Config:  config,
		Git: &GitInfo{
			Repo: "BionD", Branch: "main", Modified: 12, Untracked: 4, Ahead: 2,
		},
		Style: Style{},
		Now:   now,
	})
	gitLine := strings.Split(out, "\n")[0]
	for _, pattern := range []string{`branch main`, `!12`, `\?4`, `\^2`} {
		if !regexp.MustCompile(pattern).MatchString(gitLine) {
			t.Errorf("git line %q did not match %q", gitLine, pattern)
		}
	}
}

func TestRenderOmitsMissingData(t *testing.T) {
	_, config, now := textRenderFixture()
	var payload Payload
	payload.Model.DisplayName = "Sonnet 5"
	if got := renderHud(RenderInput{Payload: payload, Config: config, Style: Style{}, Now: now}); got != "model Sonnet 5" {
		t.Errorf("renderHud = %q", got)
	}
}

func renderWidthCase(columns int, overflow string) string {
	payload, config, now := textRenderFixture()
	if overflow != "" {
		config.Overflow = overflow
	}
	toolCalls := 108
	return renderHud(RenderInput{
		Payload:   payload,
		Config:    config,
		Scoped:    []Bucket{{ID: "fable", Label: "Fable", Percent: 22}},
		ToolCalls: &toolCalls,
		Columns:   columns,
		Style:     Style{},
		Now:       now,
	})
}

func TestRenderWidthCompactionAndShedding(t *testing.T) {
	for _, columns := range []int{200, 130, 110, 94, 80, 65, 50, 40, 30} {
		for _, line := range strings.Split(renderWidthCase(columns, ""), "\n") {
			if got := visibleWidth(line); got > columns {
				t.Errorf("width %d: line has %d cells: %q", columns, got, line)
			}
		}
	}
	roomy := renderWidthCase(200, "")
	tighter := renderWidthCase(120, "")
	if !regexp.MustCompile(`\(\d`).MatchString(roomy) || regexp.MustCompile(`\(\d`).MatchString(tighter) || !strings.Contains(tighter, "█") {
		t.Errorf("unexpected compaction: roomy=%q tighter=%q", roomy, tighter)
	}
	narrow := renderWidthCase(46, "")
	if !strings.Contains(narrow, "Opus 5") || !regexp.MustCompile(`5h .*38%`).MatchString(narrow) || strings.Contains(narrow, "tools") {
		t.Errorf("unexpected shedding: %q", narrow)
	}
}

func TestRenderNonzeroGaugeAndWrap(t *testing.T) {
	_, config, now := textRenderFixture()
	config.BarWidth = 3
	var payload Payload
	payload.RateLimits.SevenDay = &RateWindow{UsedPercentage: floatPointer(4)}
	out := renderHud(RenderInput{Payload: payload, Config: config, Style: Style{}, Now: now})
	if !strings.Contains(out, "█") {
		t.Errorf("nonzero gauge has no filled cell: %q", out)
	}

	out = renderWidthCase(60, "wrap")
	if len(strings.Split(out, "\n")) <= 1 || !strings.Contains(out, "Fable") {
		t.Errorf("wrap output = %q", out)
	}
	for _, line := range strings.Split(out, "\n") {
		if visibleWidth(line) > 60 {
			t.Errorf("wrapped line too wide: %q", line)
		}
	}
}

func TestRenderClipsGitAndSurvivesEmptyPayload(t *testing.T) {
	payload, config, now := textRenderFixture()
	out := renderHud(RenderInput{
		Payload: payload,
		Config:  config,
		Git:     &GitInfo{Repo: "r", Branch: "feature/" + strings.Repeat("x", 120)},
		Columns: 50,
		Style:   Style{},
		Now:     now,
	})
	gitLine := strings.Split(out, "\n")[0]
	if visibleWidth(gitLine) > 50 || !strings.HasSuffix(gitLine, "…") {
		t.Errorf("clipped git line = %q (%d cells)", gitLine, visibleWidth(gitLine))
	}
	if got := renderHud(RenderInput{Config: config, Style: Style{}, Now: now}); got != "" {
		t.Errorf("empty payload = %q", got)
	}
}

func TestRenderANSIParity(t *testing.T) {
	_, config, now := textRenderFixture()
	var payload Payload
	payload.Model.DisplayName = "Opus"
	got := renderHud(RenderInput{Payload: payload, Config: config, Style: Style{Enabled: true}, Now: now})
	want := "\x1b[38;2;86;95;137mmodel\x1b[0m \x1b[38;2;122;162;247mOpus\x1b[0m"
	if got != want {
		t.Errorf("ANSI render = %q, want %q", got, want)
	}
}

func TestClipRenderedCountsCells(t *testing.T) {
	for _, tt := range []struct {
		text  string
		width int
		want  string
	}{
		{"abcdef", 4, "abc…"},
		{"界界界界", 5, "界界…"},
		{"a界界界", 4, "a界…"},
		{"abc", 3, "abc"},
	} {
		if got := clipRendered(tt.text, tt.width); got != tt.want || visibleWidth(got) > tt.width {
			t.Errorf("clipRendered(%q, %d) = %q (%d cells), want %q", tt.text, tt.width, got, visibleWidth(got), tt.want)
		}
	}
}
