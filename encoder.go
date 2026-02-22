package statelog

import (
	"encoding/json"
	"fmt"
)

const (
	// TypeJSON marks a payload as JSON-encoded.
	TypeJSON byte = 0x01

	// TypeString marks a payload as a raw UTF-8 string.
	TypeString byte = 0x02
)

// Encoder defines the strategy for serializing and deserializing log payloads.
// Implement this interface to support custom data formats.
type Encoder interface {
	Encode(data any) (payload []byte, typeFlag byte, err error)
	Decode(payload []byte, typeFlag byte) (any, error)
}

// DefaultEncoder serializes strings as raw UTF-8 and everything else as JSON.
type DefaultEncoder struct{}

func (DefaultEncoder) Encode(data any) ([]byte, byte, error) {
	switch v := data.(type) {
	case string:
		return []byte(v), TypeString, nil
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return nil, 0, fmt.Errorf("statelog: json encode: %w", err)
		}
		return b, TypeJSON, nil
	}
}

func (DefaultEncoder) Decode(payload []byte, typeFlag byte) (any, error) {
	switch typeFlag {
	case TypeJSON:
		var v any
		if err := json.Unmarshal(payload, &v); err != nil {
			return nil, fmt.Errorf("statelog: json decode: %w", err)
		}
		return v, nil
	case TypeString:
		return string(payload), nil
	default:
		return nil, fmt.Errorf("statelog: unknown type flag: 0x%02x", typeFlag)
	}
}
