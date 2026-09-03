package main

import (
	"encoding/json"
	"math"
	"sort"
)

// glyphKeys lists every glyph, in the order --legend prints them.
var glyphKeys = []string{
	"model", "fiveHour", "weekly", "scoped", "context", "thinking", "session",
	"tools", "branch", "repo", "cost", "dirty", "untracked", "ahead", "behind",
	"separator",
}

// GlyphSet maps a glyph key to the string drawn for it.
type GlyphSet map[string]string

var nerdGlyphs = GlyphSet{
	"model":     "\U000F06A9", // md-robot
	"fiveHour":  "",          // fa-clock
	"weekly":    "",          // fa-calendar
	"scoped":    "",           // the model name is the label; no icon by default
	"context":   "\U000F035B", // md-memory
	"session":   "\U000F051F", // md-timer-sand
	"tools":     "",          // fa-wrench
	"thinking":  "\U000F09D1", // md-brain
	"branch":    "",          // dev-git-branch
	"repo":      "",          // oct-repo
	"cost":      "\U000F0BC5", // md-cash
	"dirty":     "\U000F06D5", // md-pencil
	"untracked": "\U000F02D6", // md-help
	"ahead":     "↑",
	"behind":    "↓",
	"separator": "│",
}

var textGlyphs = GlyphSet{
	"model":     "model",
	"fiveHour":  "5h",
	"weekly":    "wk",
	"scoped":    "",
	"context":   "ctx",
	"session":   "up",
	"tools":     "tools",
	"thinking":  "think",
	"branch":    "branch",
	"repo":      "repo",
	"cost":      "$",
	"dirty":     "!",
	"untracked": "?",
	"ahead":     "^",
	"behind":    "v",
	"separator": "|",
}

// elementMeanings is what --legend prints next to each glyph, keyed like glyphKeys.
var elementMeanings = map[string]string{
	"model":     "active model (1M marks the long-context variant)",
	"fiveHour":  "5-hour usage window, with time until it resets",
	"weekly":    "weekly usage across all models, with time until it resets",
	"scoped":    "weekly quota for one model — shown as its name (Fable, ...), no icon",
	"context":   "context window used in this session",
	"thinking":  "extended thinking, with the effort level",
	"session":   "how long this session has been running",
	"tools":     "tool calls made in this session",
	"branch":    "current git branch",
	"repo":      "repository name (off by default)",
	"cost":      "session spend in USD (off by default)",
	"dirty":     "staged / modified files",
	"untracked": "untracked files",
	"ahead":     "commits ahead of upstream",
	"behind":    "commits behind upstream",
	"separator": "divider between elements",
}

// GlyphSetting is the "glyphs" config key: the string "nerd" or "text", or an
// object of per-key overrides applied on top of the Nerd Font set.
type GlyphSetting struct {
	Mode      string // "nerd" | "text"; "" behaves as "nerd"
	Overrides map[string]string
}

// UnmarshalJSON accepts a string or an object; null means the default set.
// Any other shape leaves the setting unchanged rather than failing the whole
// config.
func (g *GlyphSetting) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*g = GlyphSetting{Mode: "nerd"}
		return nil
	}
	var mode string
	if err := json.Unmarshal(data, &mode); err == nil {
		g.Mode = mode
		g.Overrides = nil
		return nil
	}
	var overrides map[string]string
	mergeTyped(data, &overrides)
	if overrides != nil {
		g.Mode = ""
		g.Overrides = overrides
	}
	return nil
}

// mergeTyped decodes a JSON object into dst one entry at a time, keeping the
// entries whose value is a T and skipping the rest, so one wrong-typed key
// cannot discard the map. A non-object leaves dst as it was; an object
// allocates dst when nil.
func mergeTyped[T any](data []byte, dst *map[string]T) {
	var entries map[string]json.RawMessage
	if json.Unmarshal(data, &entries) != nil || entries == nil {
		return
	}
	if *dst == nil {
		*dst = make(map[string]T, len(entries))
	}
	for key, raw := range entries {
		var value T
		if string(raw) != "null" && json.Unmarshal(raw, &value) == nil {
			(*dst)[key] = value
		}
	}
}

// NameSet is the "names" config key, merged over the defaults per key.
type NameSet map[string]string

func (n *NameSet) UnmarshalJSON(data []byte) error {
	mergeTyped(data, (*map[string]string)(n))
	return nil
}

// ElementSet is the "elements" config key, merged over the defaults per key.
type ElementSet map[string]bool

func (e *ElementSet) UnmarshalJSON(data []byte) error {
	mergeTyped(data, (*map[string]bool)(e))
	return nil
}

// Stop is one positioned colour on the meter ramp, At in 0..100.
type Stop struct {
	At  float64
	RGB RGB
}

// Ramp is the "palette.bar" key: either ["#hex", ...] spread evenly across
// 0..100, or [{"at": 55, "color": "#hex"}, ...]. Stops are normalised at decode
// time (a missing or non-numeric "at" falls back to the even spread, "hex" is
// accepted for "color", a malformed colour becomes #dddddd) and stably sorted
// by At. An empty or malformed ramp decodes to zero stops, which rampColor
// treats as #dddddd.
type Ramp []Stop

func (r *Ramp) UnmarshalJSON(data []byte) error {
	var items []json.RawMessage
	if err := json.Unmarshal(data, &items); err != nil || items == nil {
		return nil
	}
	stops := make(Ramp, 0, len(items))
	denominator := math.Max(1, float64(len(items)-1))
	for i, item := range items {
		fallbackAt := float64(i) / denominator * 100
		var color string
		if err := json.Unmarshal(item, &color); err == nil {
			stops = append(stops, Stop{At: fallbackAt, RGB: parseHex(color)})
			continue
		}
		var object struct {
			At    json.RawMessage `json:"at"`
			Color json.RawMessage `json:"color"`
			Hex   json.RawMessage `json:"hex"`
		}
		at := fallbackAt
		hex := "#dddddd"
		if err := json.Unmarshal(item, &object); err == nil {
			var decodedAt float64
			if len(object.At) > 0 && string(object.At) != "null" && json.Unmarshal(object.At, &decodedAt) == nil && !math.IsNaN(decodedAt) && !math.IsInf(decodedAt, 0) {
				at = decodedAt
			}
			if len(object.Color) > 0 && string(object.Color) != "null" {
				_ = json.Unmarshal(object.Color, &hex)
			} else if len(object.Hex) > 0 && string(object.Hex) != "null" {
				_ = json.Unmarshal(object.Hex, &hex)
			}
		}
		stops = append(stops, Stop{At: at, RGB: parseHex(hex)})
	}
	sort.SliceStable(stops, func(i, j int) bool { return stops[i].At < stops[j].At })
	*r = stops
	return nil
}

// Palette holds every colour the line uses, as "#rrggbb". The user's
// "palette" object merges over defaultPalette key by key.
type Palette struct {
	Accent   string `json:"accent"`
	Muted    string `json:"muted"`
	Text     string `json:"text"`
	Bar      Ramp   `json:"bar"`
	BarEmpty string `json:"barEmpty"`
	Scoped   string `json:"scoped"`
	OK       string `json:"ok"`
	Warn     string `json:"warn"`
	Crit     string `json:"crit"`
}

// defaultPalette is Tokyo Night. The ramp holds green through 55%, warms to
// amber by 80% and reaches red at 100%.
func defaultPalette() Palette {
	return Palette{
		Accent:   "#7aa2f7",
		Muted:    "#565f89",
		Text:     "#c0caf5",
		Bar:      mustRamp(`[{"at":0,"color":"#9ece6a"},{"at":55,"color":"#9ece6a"},{"at":80,"color":"#e0af68"},{"at":100,"color":"#f7768e"}]`),
		BarEmpty: "#3b4261",
		Scoped:   "#bb9af7",
		OK:       "#9ece6a",
		Warn:     "#e0af68",
		Crit:     "#f7768e",
	}
}

func mustRamp(src string) Ramp {
	var r Ramp
	if err := json.Unmarshal([]byte(src), &r); err != nil {
		panic(err)
	}
	return r
}

// Config is the user-facing configuration. Field names and defaults mirror the
// README. Optional string keys that may be JSON null are pointers.
type Config struct {
	Glyphs          GlyphSetting `json:"glyphs"`
	Palette         Palette      `json:"palette"`
	BarWidth        int          `json:"barWidth"`
	ContextBarWidth int          `json:"contextBarWidth"`
	Labels          string       `json:"labels"` // "auto" | "always" | "never"
	Names           NameSet      `json:"names"`  // fiveHour, weekly, context
	Separator       *string      `json:"separator"`
	Overflow        string       `json:"overflow"` // "shrink" | "wrap" | "none"
	ReserveColumns  int          `json:"reserveColumns"`
	ShowGitLine     bool         `json:"showGitLine"`
	Elements        ElementSet   `json:"elements"`
	ShedOrder       []string     `json:"shedOrder"`
	UsageTTLSeconds float64      `json:"usageTtlSeconds"`
	UsageAPI        bool         `json:"usageApi"`
}

var defaultElements = ElementSet{
	"model": true, "fiveHour": true, "weekly": true, "scoped": true,
	"context": true, "session": true, "thinking": true, "tools": true,
	"cost": false, "repo": false, "branch": true, "gitStatus": true,
}

var defaultNames = NameSet{"fiveHour": "5h", "weekly": "wk", "context": "ctx"}

// defaultShedOrder is the order elements are given up when even the tightest
// layout does not fit: least useful first, the quota meters last.
var defaultShedOrder = []string{"cost", "tools", "session", "thinking", "context", "scoped", "weekly", "fiveHour", "model"}

func defaultConfig() Config {
	elements := make(ElementSet, len(defaultElements))
	for k, v := range defaultElements {
		elements[k] = v
	}
	names := make(NameSet, len(defaultNames))
	for k, v := range defaultNames {
		names[k] = v
	}
	return Config{
		Glyphs:          GlyphSetting{Mode: "nerd"},
		Palette:         defaultPalette(),
		BarWidth:        8,
		ContextBarWidth: 10,
		Labels:          "auto",
		Names:           names,
		Overflow:        "shrink",
		ReserveColumns:  2,
		ShowGitLine:     true,
		Elements:        elements,
		UsageTTLSeconds: 120,
		UsageAPI:        true,
	}
}

// element reports whether an element is enabled, falling back to the default
// for a key the map does not carry.
func (c Config) element(key string) bool {
	if v, ok := c.Elements[key]; ok {
		return v
	}
	return defaultElements[key]
}

// name is the display name for a labelled meter, falling back to the default.
func (c Config) name(key string) string {
	if v, ok := c.Names[key]; ok {
		return v
	}
	return defaultNames[key]
}

// resolveGlyphs returns a fresh GlyphSet for the setting: the text set for
// "text", otherwise the Nerd Font set with the overrides applied.
func resolveGlyphs(setting GlyphSetting) GlyphSet {
	base := nerdGlyphs
	if setting.Mode == "text" {
		base = textGlyphs
	}
	resolved := make(GlyphSet, len(base)+len(setting.Overrides))
	for key, glyph := range base {
		resolved[key] = glyph
	}
	if setting.Mode != "text" {
		for key, glyph := range setting.Overrides {
			resolved[key] = glyph
		}
	}
	return resolved
}
