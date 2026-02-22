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
	switch str := data.(type) {
	case string:
		return []byte(str), TypeString, nil
	default:
		encoded, err := json.Marshal(data)
		if err != nil {
			return nil, 0, fmt.Errorf("statelog: json encode: %w", err)
		}
		return encoded, TypeJSON, nil
	}
}

func (DefaultEncoder) Decode(payload []byte, typeFlag byte) (any, error) {
	switch typeFlag {
	case TypeJSON:
		var decoded any
		if err := json.Unmarshal(payload, &decoded); err != nil {
			return nil, fmt.Errorf("statelog: json decode: %w", err)
		}
		return decoded, nil
	case TypeString:
		return string(payload), nil
	default:
		return nil, fmt.Errorf("statelog: unknown type flag: 0x%02x", typeFlag)
	}
}
