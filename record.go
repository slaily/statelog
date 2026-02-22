package statelog

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"
	"reflect"
)

// encodeRecord serializes a struct value into a fixed-size byte slice
// according to the schema. Returns exactly RecordSize bytes.
func encodeRecord[T any](schema *Schema, data T) ([]byte, error) {
	buf := make([]byte, schema.RecordSize)
	v := reflect.ValueOf(data)

	for i, field := range schema.Fields {
		fv := v.Field(i)
		off := int(field.Offset)

		switch field.Type {
		case TypeString:
			s := fv.String()
			if len(s) > int(field.Size) {
				return nil, fmt.Errorf("statelog: field %q: value length %d exceeds max %d", field.Name, len(s), field.Size)
			}
			copy(buf[off:], s)

		case TypeUint8:
			buf[off] = uint8(fv.Uint())

		case TypeUint16:
			binary.LittleEndian.PutUint16(buf[off:], uint16(fv.Uint()))

		case TypeUint32:
			binary.LittleEndian.PutUint32(buf[off:], uint32(fv.Uint()))

		case TypeUint64:
			binary.LittleEndian.PutUint64(buf[off:], fv.Uint())

		case TypeInt64:
			binary.LittleEndian.PutUint64(buf[off:], uint64(fv.Int()))

		case TypeFloat64:
			binary.LittleEndian.PutUint64(buf[off:], math.Float64bits(fv.Float()))

		case TypeBool:
			if fv.Bool() {
				buf[off] = 0x01
			}

		case TypeBytes:
			src := fv.Slice(0, fv.Len()).Bytes()
			copy(buf[off:], src)
		}
	}

	checksum := crc32.ChecksumIEEE(buf[:schema.DataSize])
	binary.LittleEndian.PutUint32(buf[schema.DataSize:], checksum)

	return buf, nil
}

// decodeRecord deserializes a fixed-size byte slice into a struct value.
// Returns a CorruptionError if the CRC32 checksum doesn't match.
func decodeRecord[T any](schema *Schema, buf []byte, offset int64) (T, error) {
	var zero T
	if uint32(len(buf)) < schema.RecordSize {
		return zero, &CorruptionError{
			Offset: offset,
			Reason: "buffer too small for record",
		}
	}

	expectedCRC := binary.LittleEndian.Uint32(buf[schema.DataSize:])
	actualCRC := crc32.ChecksumIEEE(buf[:schema.DataSize])
	if expectedCRC != actualCRC {
		return zero, &CorruptionError{
			Offset: offset,
			Reason: "CRC32 checksum mismatch",
		}
	}

	var result T
	v := reflect.ValueOf(&result).Elem()

	for i, field := range schema.Fields {
		fv := v.Field(i)
		off := int(field.Offset)

		switch field.Type {
		case TypeString:
			raw := buf[off : off+int(field.Size)]
			fv.SetString(string(bytes.TrimRight(raw, "\x00")))

		case TypeUint8:
			fv.SetUint(uint64(buf[off]))

		case TypeUint16:
			fv.SetUint(uint64(binary.LittleEndian.Uint16(buf[off:])))

		case TypeUint32:
			fv.SetUint(uint64(binary.LittleEndian.Uint32(buf[off:])))

		case TypeUint64:
			fv.SetUint(binary.LittleEndian.Uint64(buf[off:]))

		case TypeInt64:
			fv.SetInt(int64(binary.LittleEndian.Uint64(buf[off:])))

		case TypeFloat64:
			fv.SetFloat(math.Float64frombits(binary.LittleEndian.Uint64(buf[off:])))

		case TypeBool:
			fv.SetBool(buf[off] != 0)

		case TypeBytes:
			arr := fv.Slice(0, fv.Len())
			copy(arr.Bytes(), buf[off:off+int(field.Size)])
		}
	}

	return result, nil
}

// extractField returns the raw bytes of the field at fieldIndex within buf.
// Zero-allocation — returns a sub-slice of buf.
func extractField(schema *Schema, fieldIndex int, buf []byte) []byte {
	f := &schema.Fields[fieldIndex]
	return buf[f.Offset : f.Offset+f.Size]
}

// encodeKey converts a Go value to its on-disk byte representation for
// the field at fieldIndex. Used by the querier for hashing lookup keys.
func encodeKey(schema *Schema, fieldIndex int, key any) ([]byte, error) {
	f := &schema.Fields[fieldIndex]

	switch f.Type {
	case TypeString:
		s, ok := key.(string)
		if !ok {
			return nil, fmt.Errorf("statelog: field %q is string, got %T", f.Name, key)
		}
		if len(s) > int(f.Size) {
			return nil, fmt.Errorf("statelog: field %q: key length %d exceeds max %d", f.Name, len(s), f.Size)
		}
		buf := make([]byte, f.Size)
		copy(buf, s)
		return buf, nil

	case TypeUint8:
		v, ok := key.(uint8)
		if !ok {
			return nil, fmt.Errorf("statelog: field %q is uint8, got %T", f.Name, key)
		}
		return []byte{v}, nil

	case TypeUint16:
		v, ok := key.(uint16)
		if !ok {
			return nil, fmt.Errorf("statelog: field %q is uint16, got %T", f.Name, key)
		}
		buf := make([]byte, 2)
		binary.LittleEndian.PutUint16(buf, v)
		return buf, nil

	case TypeUint32:
		v, ok := key.(uint32)
		if !ok {
			return nil, fmt.Errorf("statelog: field %q is uint32, got %T", f.Name, key)
		}
		buf := make([]byte, 4)
		binary.LittleEndian.PutUint32(buf, v)
		return buf, nil

	case TypeUint64:
		v, ok := key.(uint64)
		if !ok {
			return nil, fmt.Errorf("statelog: field %q is uint64, got %T", f.Name, key)
		}
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, v)
		return buf, nil

	case TypeInt64:
		v, ok := key.(int64)
		if !ok {
			return nil, fmt.Errorf("statelog: field %q is int64, got %T", f.Name, key)
		}
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, uint64(v))
		return buf, nil

	case TypeFloat64:
		v, ok := key.(float64)
		if !ok {
			return nil, fmt.Errorf("statelog: field %q is float64, got %T", f.Name, key)
		}
		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, math.Float64bits(v))
		return buf, nil

	case TypeBool:
		v, ok := key.(bool)
		if !ok {
			return nil, fmt.Errorf("statelog: field %q is bool, got %T", f.Name, key)
		}
		if v {
			return []byte{0x01}, nil
		}
		return []byte{0x00}, nil

	case TypeBytes:
		v, ok := key.([]byte)
		if !ok {
			return nil, fmt.Errorf("statelog: field %q is [N]byte, got %T", f.Name, key)
		}
		buf := make([]byte, f.Size)
		copy(buf, v)
		return buf, nil

	default:
		return nil, fmt.Errorf("statelog: unknown field type 0x%02x", f.Type)
	}
}
