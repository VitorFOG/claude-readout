package main

import (
	"encoding/json"
	"errors"
	"os"
)

// LoadedConfig is the user config merged over the defaults, plus where it came
// from and why it may have been rejected (surfaced by --doctor only).
type LoadedConfig struct {
	Config Config
	Path   string
	// Err is non-nil when the file exists but could not be used in full. A
	// syntax error leaves Config at the defaults (json.Unmarshal checks the
	// whole input before writing anything). A type error (a key of the wrong
	// shape) keeps everything that did decode.
	Err error
}

// loadConfig reads the config file, if any, over defaultConfig().
//
// Merge semantics match the Node version: objects merge key by key (nested
// structs and maps), arrays and scalars from the file replace the default.
// Decoding the file into a Config that already holds the defaults gives exactly
// that. A missing file is not an error.
func loadConfig(env Env) LoadedConfig {
	path := env.configPath()
	config := defaultConfig()
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return LoadedConfig{Config: config, Path: path}
	}
	if err != nil {
		return LoadedConfig{Config: config, Path: path, Err: err}
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return LoadedConfig{Config: config, Path: path, Err: err}
	}
	return LoadedConfig{Config: config, Path: path}
}
