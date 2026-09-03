// claude-readout is a Nerd Font statusline for Claude Code.
//
// Contract: read Claude Code's statusline JSON on stdin, print the line(s) on
// stdout, exit 0. Anything that goes wrong is swallowed and the process still
// prints something useful: a statusline that fails is a statusline that blanks
// the pane.
package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

func main() {
	defer func() {
		if r := recover(); r != nil {
			os.Stdout.WriteString("readout: " + panicMessage(r) + "\n")
		}
		os.Exit(0)
	}()
	run(os.Args[1:], os.Stdin, os.Stdout, envFromOS(), os.Getenv, time.Now())
}

func panicMessage(r any) string {
	if err, ok := r.(error); ok {
		return err.Error()
	}
	if s, ok := r.(string); ok {
		return s
	}
	return "unexpected failure"
}

// run is the whole CLI, with every process dependency passed in so tests can
// drive it directly. Without a flag it renders one frame from stdin. Flags:
//
//	--refresh-usage [--lock-held]  fetch the usage snapshot now and exit
//	--legend | --glyphs            print every element with its glyph and
//	                               meaning; doubles as a font check
//	--ramp                         print the meter ramp at 5% steps
//	--doctor                       binary and config paths, token, cache age, colour
//	--setup                        point Claude Code's settings at this binary
//	--no-color                     force colour off
func run(args []string, stdin io.Reader, stdout io.Writer, env Env, getenv func(string) string, now time.Time) {
	hasFlag := func(flag string) bool {
		for _, arg := range args {
			if arg == flag {
				return true
			}
		}
		return false
	}
	if hasFlag("--refresh-usage") {
		refreshUsage(env, hasFlag("--lock-held"), fetchUsage, now)
		return
	}
	if hasFlag("--legend") || hasFlag("--glyphs") {
		printLegend(stdout)
		return
	}

	loaded := loadConfig(env)
	if hasFlag("--ramp") {
		style := Style{Enabled: detectColorSupport(getenv) && !hasFlag("--no-color")}
		printRamp(stdout, loaded.Config, style)
		return
	}
	if hasFlag("--doctor") {
		printDoctor(stdout, loaded, env, getenv, now)
		return
	}
	if hasFlag("--setup") {
		runSetup(stdout, env)
		return
	}

	data, err := io.ReadAll(stdin)
	if err != nil {
		data = nil
	}
	payload := parsePayload(data)
	cwd := payload.Workspace.CurrentDir
	if cwd == "" {
		cwd = payload.Cwd
	}
	if cwd == "" {
		cwd, _ = os.Getwd()
	}

	var cached *UsageCache
	if loaded.Config.UsageAPI {
		cached = readCachedUsage(env)
		if isStale(cached, loaded.Config.UsageTTLSeconds, now) {
			spawnRefresh(env)
		}
	}
	var git *GitInfo
	if loaded.Config.ShowGitLine {
		git = readGit(cwd, loaded.Config.element("repo"))
	}
	var toolCalls *int
	if loaded.Config.element("tools") {
		if count, ok := countToolCalls(payload.TranscriptPath, payload.SessionID, env); ok {
			toolCalls = &count
		}
	}
	columns := 0
	for _, key := range []string{"READOUT_COLUMNS", "COLUMNS"} {
		value, parseErr := strconv.ParseFloat(strings.TrimSpace(getenv(key)), 64)
		if parseErr == nil && value != 0 && !math.IsNaN(value) && !math.IsInf(value, 0) {
			columns = int(value)
			break
		}
	}
	scoped := []Bucket{}
	if cached != nil {
		scoped = cached.Scoped
	}
	out := renderHud(RenderInput{
		Payload:   payload,
		Config:    loaded.Config,
		Git:       git,
		Scoped:    scoped,
		ToolCalls: toolCalls,
		Columns:   columns,
		Style:     Style{Enabled: detectColorSupport(getenv) && !hasFlag("--no-color")},
		Now:       now,
	})
	_, _ = io.WriteString(stdout, out+"\n")
}

// spawnRefresh claims the lock and starts a detached `<self> --refresh-usage
// --lock-held` with stdio discarded, then returns without waiting. If the
// child cannot be started the lock is abandoned. The lock is claimed here, not
// in the child, so frames arriving during the child's startup do not each
// spawn a refresher of their own.
func spawnRefresh(env Env) {
	if !claimRefresh(env) {
		return
	}
	self, err := os.Executable()
	if err != nil {
		abandonRefresh(env)
		return
	}
	cmd := exec.Command(self, "--refresh-usage", "--lock-held")
	cmd.SysProcAttr = detachAttrs()
	if err := cmd.Start(); err != nil {
		abandonRefresh(env)
		return
	}
	_ = cmd.Process.Release()
}

// printLegend lists every element with its glyph, code point and meaning; a
// box in the output means the terminal font lacks that glyph.
func printLegend(w io.Writer) {
	_, _ = io.WriteString(w, "Element legend — also a font check (boxes mean a missing glyph)\n\n")
	for _, key := range glyphKeys {
		glyph := nerdGlyphs[key]
		shown := glyph
		codePoint := "text"
		if glyph == "" {
			shown = "·"
		} else {
			for _, firstRune := range glyph {
				codePoint = fmt.Sprintf("U+%04X", firstRune)
				break
			}
		}
		_, _ = fmt.Fprintf(w, "  %s   %-10s %-7s %s\n", shown, key, codePoint, elementMeanings[key])
	}
	_, _ = io.WriteString(w, "\nOverride any glyph in config.json under \"glyphs\", or set \"glyphs\": \"text\"\n")
	_, _ = io.WriteString(w, "for plain labels. \"labels\": \"always\" names every meter inline.\n")
}

// printRamp shows the meter at 5% steps so the user can see the palette they
// are tuning.
func printRamp(w io.Writer, cfg Config, style Style) {
	_, _ = io.WriteString(w, "Meter ramp at the configured palette:\n\n")
	for percent := 0; percent <= 100; percent += 5 {
		rgb := rampColor(float64(percent), cfg.Palette.Bar)
		hex := fmt.Sprintf("#%02x%02x%02x", int(math.Round(rgb[0])), int(math.Round(rgb[1])), int(math.Round(rgb[2])))
		filledCount := int(math.Round(float64(percent) / 100 * float64(cfg.BarWidth)))
		filled := strings.Repeat(barFilled, filledCount)
		empty := strings.Repeat(barEmpty, cfg.BarWidth-filledCount)
		bar := style.fgRGB(rgb, filled) + style.fg(cfg.Palette.BarEmpty, empty)
		annotation := ""
		switch percent {
		case 55:
			annotation = "   <- green holds to here"
		case 80:
			annotation = "   <- amber"
		case 100:
			annotation = "   <- red"
		}
		_, _ = fmt.Fprintf(w, "  %3d%%  %s  %s%s\n", percent, bar, style.fgRGB(rgb, hex), annotation)
	}
	_, _ = io.WriteString(w, "\nTune it with \"palette\": { \"bar\": [ { \"at\": 0, \"color\": \"#...\" }, ... ] }\n")
}

// executablePath is os.Executable, or "" when the OS cannot say.
func executablePath() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	return self
}

// printDoctor reports where the binary, config, token and usage cache are, and
// whether colour is on, for the "why is my line blank" questions.
func printDoctor(w io.Writer, loaded LoadedConfig, env Env, getenv func(string) string, now time.Time) {
	_, _ = fmt.Fprintf(w, "binary:      %s\n", executablePath())
	invalid := ""
	if loaded.Err != nil {
		invalid = "  (INVALID: " + loaded.Err.Error() + ")"
	}
	_, _ = fmt.Fprintf(w, "config:      %s%s\n", loaded.Path, invalid)
	if _, ok := readAccessToken(env, now); ok {
		_, _ = io.WriteString(w, "oauth token: found\n")
	} else {
		_, _ = io.WriteString(w, "oauth token: not found (scoped buckets hidden)\n")
	}
	if cached := readCachedUsage(env); cached != nil {
		age := int64(math.Round(float64(now.UnixMilli()-cached.FetchedAt) / 1000))
		_, _ = fmt.Fprintf(w, "usage cache: %d scoped bucket(s), %ds old\n", len(cached.Scoped), age)
	} else {
		_, _ = io.WriteString(w, "usage cache: empty (run once more, or --refresh-usage)\n")
	}
	color := "disabled"
	if detectColorSupport(getenv) {
		color = "enabled"
	}
	_, _ = fmt.Fprintf(w, "color:       %s\n", color)
}
