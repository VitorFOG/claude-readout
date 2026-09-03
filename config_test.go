package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestConfigDeepMerge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := `{
		"palette":{"accent":"#010203"},
		"elements":{"tools":false},
		"glyphs":{"model":"bot"}
	}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := loadConfig(Env{ReadoutConfig: path})
	if loaded.Err != nil {
		t.Fatalf("loadConfig error: %v", loaded.Err)
	}
	if loaded.Path != path {
		t.Errorf("path = %q, want %q", loaded.Path, path)
	}
	if loaded.Config.Palette.Accent != "#010203" || loaded.Config.Palette.Muted != "#565f89" {
		t.Errorf("palette did not merge: %+v", loaded.Config.Palette)
	}
	if loaded.Config.element("tools") || !loaded.Config.element("model") {
		t.Errorf("elements did not merge: %v", loaded.Config.Elements)
	}
	glyphs := resolveGlyphs(loaded.Config.Glyphs)
	if glyphs["model"] != "bot" || glyphs["weekly"] != nerdGlyphs["weekly"] {
		t.Errorf("glyphs did not merge over nerd defaults: %v", glyphs)
	}
}

func TestConfigSyntaxErrorUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"barWidth":`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := loadConfig(Env{ReadoutConfig: path})
	var syntax *json.SyntaxError
	if !errors.As(loaded.Err, &syntax) {
		t.Fatalf("error = %T %v, want *json.SyntaxError", loaded.Err, loaded.Err)
	}
	if loaded.Config.BarWidth != defaultConfig().BarWidth || !reflect.DeepEqual(loaded.Config.Palette, defaultConfig().Palette) {
		t.Errorf("syntax error config = %+v, want defaults", loaded.Config)
	}
}

func TestConfigTypeErrorKeepsPartialDecode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"barWidth":"wide","labels":"always"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded := loadConfig(Env{ReadoutConfig: path})
	var typeErr *json.UnmarshalTypeError
	if !errors.As(loaded.Err, &typeErr) {
		t.Fatalf("error = %T %v, want *json.UnmarshalTypeError", loaded.Err, loaded.Err)
	}
	if loaded.Config.BarWidth != 8 || loaded.Config.Labels != "always" {
		t.Errorf("partial config = %+v", loaded.Config)
	}
}

func TestConfigMissingFileUsesDefaults(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.json")
	loaded := loadConfig(Env{ReadoutConfig: path})
	if loaded.Err != nil {
		t.Fatalf("missing config error: %v", loaded.Err)
	}
	if loaded.Path != path || loaded.Config.BarWidth != 8 || loaded.Config.Palette.Accent != "#7aa2f7" {
		t.Errorf("missing config result = %+v", loaded)
	}
}

func TestConfigCustomUnmarshalShapes(t *testing.T) {
	glyphs := GlyphSetting{Mode: "text"}
	if err := json.Unmarshal([]byte(`null`), &glyphs); err != nil {
		t.Fatal(err)
	}
	if glyphs.Mode != "nerd" || glyphs.Overrides != nil {
		t.Errorf("null should reset to the Nerd Font set as the Node version did, got %+v", glyphs)
	}
	if err := json.Unmarshal([]byte(`42`), &glyphs); err != nil {
		t.Fatal(err)
	}
	if glyphs.Mode != "nerd" {
		t.Errorf("wrong-shaped glyph setting changed value: %+v", glyphs)
	}

	ramp := mustRamp(`[
		{"at":20,"color":"#000000"},
		{"at":null,"hex":"#777777"},
		{"at":80,"color":"#ffffff"}
	]`)
	if len(ramp) != 3 || ramp[0].At != 20 || ramp[1].At != 50 || ramp[2].At != 80 || ramp[1].RGB != (RGB{119, 119, 119}) {
		t.Errorf("normalized ramp = %+v", ramp)
	}
	original := append(Ramp(nil), ramp...)
	if err := json.Unmarshal([]byte(`{"not":"an array"}`), &ramp); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ramp, original) {
		t.Errorf("wrong-shaped ramp changed value: %+v", ramp)
	}
}

func TestTimestampShapes(t *testing.T) {
	for _, tt := range []struct {
		name string
		data string
		want time.Time
	}{
		{"number", `1700000000.25`, time.Unix(1700000000, 250000000)},
		{"string", `"2026-09-02T22:00:00.125Z"`, time.Date(2026, 9, 2, 22, 0, 0, 125000000, time.UTC)},
		{"quoted number", `"1788386400"`, time.Time{}},
		{"null", `null`, time.Time{}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var got Timestamp
			if err := json.Unmarshal([]byte(tt.data), &got); err != nil {
				t.Fatal(err)
			}
			if !got.Time.Equal(tt.want) {
				t.Errorf("time = %v, want %v", got.Time, tt.want)
			}
			marshaled, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			if string(marshaled) != tt.data {
				t.Errorf("marshal = %s, want %s", marshaled, tt.data)
			}
		})
	}

	original := Timestamp{Time: time.Unix(1, 0), Raw: json.RawMessage(`1`)}
	got := original
	if err := json.Unmarshal([]byte(`{"garbage":true}`), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Time.Equal(original.Time) || string(got.Raw) != string(original.Raw) {
		t.Errorf("garbage changed timestamp: %+v", got)
	}
}

func TestPayloadGarbageAndPartialTypeError(t *testing.T) {
	if got := parsePayload([]byte(`not json`)); !reflect.DeepEqual(got, Payload{}) {
		t.Errorf("garbage payload = %+v", got)
	}
	got := parsePayload([]byte(`{"session_id":"s1","cwd":42,"model":{"display_name":"Opus"}}`))
	if got.SessionID != "s1" || got.Model.DisplayName != "Opus" || got.Cwd != "" {
		t.Errorf("partial payload = %+v", got)
	}
}

func TestConfigWrongTypedEntriesFallBackPerKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := `{
		"names":{"fiveHour":null,"weekly":7,"context":"c"},
		"elements":{"model":null,"cost":"yes","tools":false},
		"glyphs":{"model":"M","fiveHour":5,"weekly":null}
	}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(Env{ReadoutConfig: path}).Config
	if cfg.name("fiveHour") != "5h" || cfg.name("weekly") != "wk" || cfg.name("context") != "c" {
		t.Errorf("names = %v", cfg.Names)
	}
	if !cfg.element("model") || cfg.element("cost") || cfg.element("tools") {
		t.Errorf("elements = %v", cfg.Elements)
	}
	glyphs := resolveGlyphs(cfg.Glyphs)
	if glyphs["model"] != "M" || glyphs["fiveHour"] != nerdGlyphs["fiveHour"] || glyphs["weekly"] != nerdGlyphs["weekly"] {
		t.Errorf("glyphs = %v", glyphs)
	}
}
