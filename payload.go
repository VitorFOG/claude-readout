package main

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"time"
)

// Timestamp is a point in time Claude Code or the usage API hands us as either
// epoch seconds (a JSON number) or an RFC 3339 string. The zero value means
// unknown. Raw keeps the original JSON so a cached bucket round-trips byte for
// byte with what the Node version wrote.
type Timestamp struct {
	Time time.Time
	Raw  json.RawMessage
}

func (t Timestamp) IsZero() bool { return t.Time.IsZero() }

// UnmarshalJSON accepts a number (epoch seconds, fractional allowed), a string
// parseable as RFC 3339 (with or without fractional seconds), or null. Any other
// value, a quoted number included, is unknown, not an error.
func (t *Timestamp) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		t.Time = time.Time{}
		t.Raw = append(t.Raw[:0], data...)
		return nil
	}
	if data[0] != '"' {
		var number json.Number
		if err := json.Unmarshal(data, &number); err == nil {
			seconds, err := strconv.ParseFloat(number.String(), 64)
			if err == nil && !math.IsNaN(seconds) && !math.IsInf(seconds, 0) {
				whole, fractional := math.Modf(seconds)
				t.Time = time.Unix(int64(whole), int64(math.Round(fractional*1e9)))
				t.Raw = append(t.Raw[:0], data...)
				return nil
			}
		}
	}
	var value string
	if err := json.Unmarshal(data, &value); err == nil {
		parsed, err := time.Parse(time.RFC3339Nano, value)
		t.Time = time.Time{}
		t.Raw = append(t.Raw[:0], data...)
		if err == nil {
			t.Time = parsed
		}
	}
	return nil
}

// MarshalJSON writes Raw when set, else null.
func (t Timestamp) MarshalJSON() ([]byte, error) {
	if len(t.Raw) == 0 {
		return []byte("null"), nil
	}
	return t.Raw, nil
}

// Payload is the JSON Claude Code writes to the statusline's stdin. Only the
// fields the line uses are declared; a missing field is its zero value, and
// numbers that may be absent are pointers so "absent" and 0 stay distinct.
type Payload struct {
	SessionID      string `json:"session_id"`
	TranscriptPath string `json:"transcript_path"`
	Cwd            string `json:"cwd"`
	Model          struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
	} `json:"model"`
	Workspace struct {
		CurrentDir string `json:"current_dir"`
	} `json:"workspace"`
	Cost struct {
		TotalCostUSD    *float64 `json:"total_cost_usd"`
		TotalDurationMs *float64 `json:"total_duration_ms"`
	} `json:"cost"`
	ContextWindow struct {
		ContextWindowSize float64  `json:"context_window_size"`
		UsedPercentage    *float64 `json:"used_percentage"`
	} `json:"context_window"`
	Thinking struct {
		Enabled bool `json:"enabled"`
	} `json:"thinking"`
	Effort struct {
		Level string `json:"level"`
	} `json:"effort"`
	RateLimits struct {
		FiveHour *RateWindow `json:"five_hour"`
		SevenDay *RateWindow `json:"seven_day"`
	} `json:"rate_limits"`
}

// RateWindow is one usage window from rate_limits.
type RateWindow struct {
	UsedPercentage *float64  `json:"used_percentage"`
	ResetsAt       Timestamp `json:"resets_at"`
}

// parsePayload decodes stdin. Garbage, empty input, or JSON null all yield an
// empty Payload: the line still renders with whatever it has. A type mismatch
// in one field keeps every other field.
func parsePayload(data []byte) Payload {
	var payload Payload
	if err := json.Unmarshal(data, &payload); err != nil {
		var typeErr *json.UnmarshalTypeError
		if !errors.As(err, &typeErr) {
			return Payload{}
		}
	}
	return payload
}
