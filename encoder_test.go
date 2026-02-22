package statelog

import (
	"strings"
	"testing"
)

func TestDefaultEncoder_EncodeString(t *testing.T) {
	var encoder DefaultEncoder

	tests := []struct {
		name  string
		input string
	}{
		{"simple", "hello world"},
		{"empty", ""},
		{"unicode", "こんにちは世界"},
		{"with newlines", "line1\nline2\n"},
		{"with special chars", `tabs	and "quotes" and \backslash`},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload, typeFlag, err := encoder.Encode(tc.input)
			if err != nil {
				t.Fatalf("Encode(%q): unexpected error: %v", tc.input, err)
			}
			if typeFlag != TypeString {
				t.Errorf("expected TypeString (0x%02x), got 0x%02x", TypeString, typeFlag)
			}
			if string(payload) != tc.input {
				t.Errorf("expected payload %q, got %q", tc.input, string(payload))
			}
		})
	}
}

func TestDefaultEncoder_EncodeJSON(t *testing.T) {
	var encoder DefaultEncoder

	tests := []struct {
		name  string
		input any
	}{
		{"map", map[string]any{"key": "value"}},
		{"slice", []any{1.0, 2.0, 3.0}},
		{"int", 42},
		{"float", 3.14},
		{"bool", true},
		{"nil", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload, typeFlag, err := encoder.Encode(tc.input)
			if err != nil {
				t.Fatalf("Encode(%v): unexpected error: %v", tc.input, err)
			}
			if typeFlag != TypeJSON {
				t.Errorf("expected TypeJSON (0x%02x), got 0x%02x", TypeJSON, typeFlag)
			}
			if len(payload) == 0 && tc.input != nil {
				t.Error("expected non-empty payload")
			}
		})
	}
}

func TestDefaultEncoder_EncodeJSONError(t *testing.T) {
	var encoder DefaultEncoder

	// Channels cannot be marshaled to JSON.
	channel := make(chan int)
	_, _, err := encoder.Encode(channel)
	if err == nil {
		t.Fatal("expected error encoding channel, got nil")
	}
	if !strings.Contains(err.Error(), "statelog: json encode") {
		t.Errorf("expected wrapped error with prefix, got: %v", err)
	}
}

func TestDefaultEncoder_DecodeString(t *testing.T) {
	var encoder DefaultEncoder

	input := "hello world"
	result, err := encoder.Decode([]byte(input), TypeString)
	if err != nil {
		t.Fatalf("Decode: unexpected error: %v", err)
	}

	str, ok := result.(string)
	if !ok {
		t.Fatalf("expected string, got %T", result)
	}
	if str != input {
		t.Errorf("expected %q, got %q", input, str)
	}
}

func TestDefaultEncoder_DecodeJSON(t *testing.T) {
	var encoder DefaultEncoder

	payload := []byte(`{"event":"click","count":5}`)
	result, err := encoder.Decode(payload, TypeJSON)
	if err != nil {
		t.Fatalf("Decode: unexpected error: %v", err)
	}

	decoded, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", result)
	}
	if decoded["event"] != "click" {
		t.Errorf("expected event=click, got %v", decoded["event"])
	}
	if decoded["count"] != float64(5) {
		t.Errorf("expected count=5, got %v", decoded["count"])
	}
}

func TestDefaultEncoder_DecodeJSONError(t *testing.T) {
	var encoder DefaultEncoder

	_, err := encoder.Decode([]byte(`{invalid json`), TypeJSON)
	if err == nil {
		t.Fatal("expected error decoding invalid JSON, got nil")
	}
	if !strings.Contains(err.Error(), "statelog: json decode") {
		t.Errorf("expected wrapped error with prefix, got: %v", err)
	}
}

func TestDefaultEncoder_DecodeUnknownTypeFlag(t *testing.T) {
	var encoder DefaultEncoder

	_, err := encoder.Decode([]byte("data"), 0xFF)
	if err == nil {
		t.Fatal("expected error for unknown type flag, got nil")
	}
	if !strings.Contains(err.Error(), "unknown type flag") {
		t.Errorf("expected 'unknown type flag' in error, got: %v", err)
	}
	if !strings.Contains(err.Error(), "0xff") {
		t.Errorf("expected hex flag in error, got: %v", err)
	}
}

func TestDefaultEncoder_RoundTrip(t *testing.T) {
	var encoder DefaultEncoder

	tests := []struct {
		name  string
		input any
		check func(t *testing.T, result any)
	}{
		{
			name:  "string",
			input: "round trip test",
			check: func(t *testing.T, result any) {
				t.Helper()
				if result != "round trip test" {
					t.Errorf("expected 'round trip test', got %v", result)
				}
			},
		},
		{
			name:  "empty string",
			input: "",
			check: func(t *testing.T, result any) {
				t.Helper()
				if result != "" {
					t.Errorf("expected empty string, got %v", result)
				}
			},
		},
		{
			name:  "map",
			input: map[string]any{"a": float64(1), "b": "two"},
			check: func(t *testing.T, result any) {
				t.Helper()
				resultMap, ok := result.(map[string]any)
				if !ok {
					t.Fatalf("expected map, got %T", result)
				}
				if resultMap["a"] != float64(1) {
					t.Errorf("expected a=1, got %v", resultMap["a"])
				}
				if resultMap["b"] != "two" {
					t.Errorf("expected b=two, got %v", resultMap["b"])
				}
			},
		},
		{
			name:  "slice",
			input: []any{"x", float64(2), true},
			check: func(t *testing.T, result any) {
				t.Helper()
				resultSlice, ok := result.([]any)
				if !ok {
					t.Fatalf("expected slice, got %T", result)
				}
				if len(resultSlice) != 3 {
					t.Fatalf("expected 3 elements, got %d", len(resultSlice))
				}
				if resultSlice[0] != "x" {
					t.Errorf("expected resultSlice[0]=x, got %v", resultSlice[0])
				}
			},
		},
		{
			name:  "bool",
			input: true,
			check: func(t *testing.T, result any) {
				t.Helper()
				if result != true {
					t.Errorf("expected true, got %v", result)
				}
			},
		},
		{
			name:  "nil",
			input: nil,
			check: func(t *testing.T, result any) {
				t.Helper()
				if result != nil {
					t.Errorf("expected nil, got %v", result)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			payload, typeFlag, err := encoder.Encode(tc.input)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}

			result, err := encoder.Decode(payload, typeFlag)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}

			tc.check(t, result)
		})
	}
}
