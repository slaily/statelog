package statelog

import (
	"fmt"
	"reflect"
	"strconv"
)

const (
	TypeString  byte = 0x01
	TypeUint8   byte = 0x02
	TypeUint16  byte = 0x03
	TypeUint32  byte = 0x04
	TypeUint64  byte = 0x05
	TypeInt64   byte = 0x06
	TypeFloat64 byte = 0x07
	TypeBool    byte = 0x08
	TypeBytes   byte = 0x09

	crc32Size = 4
	tagName   = "sl"
)

// FieldDef describes a single field in the fixed-size record schema.
type FieldDef struct {
	Name   string
	Type   byte
	Size   uint16
	Offset uint16
}

// Schema describes the fixed-size record layout derived from a Go struct.
type Schema struct {
	Fields       []FieldDef
	RecordSize   uint32
	DataSize     uint32
	fieldsByName map[string]int
}

// FieldIndex returns the index of the named field, or -1 if not found.
func (s *Schema) FieldIndex(name string) int {
	idx, ok := s.fieldsByName[name]
	if !ok {
		return -1
	}
	return idx
}

// buildSchema derives a Schema from the exported fields of struct type T.
// String fields require an `sl:"N"` tag declaring the max byte size.
// Numeric types and [N]byte arrays have implicit sizes.
func buildSchema[T any]() (*Schema, error) {
	var zero T
	t := reflect.TypeOf(zero)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil, fmt.Errorf("statelog: schema type must be a struct, got %s", t.Kind())
	}

	fields := make([]FieldDef, 0, t.NumField())
	var offset uint16

	for i := 0; i < t.NumField(); i++ {
		sf := t.Field(i)
		if !sf.IsExported() {
			continue
		}

		fd := FieldDef{Name: sf.Name, Offset: offset}

		switch sf.Type.Kind() {
		case reflect.String:
			tag := sf.Tag.Get(tagName)
			if tag == "" {
				return nil, fmt.Errorf("statelog: string field %q requires `sl:\"N\"` tag for max byte size", sf.Name)
			}
			size, err := strconv.ParseUint(tag, 10, 16)
			if err != nil || size == 0 {
				return nil, fmt.Errorf("statelog: field %q: invalid sl tag %q (must be a positive integer)", sf.Name, tag)
			}
			fd.Type = TypeString
			fd.Size = uint16(size)

		case reflect.Uint8:
			fd.Type = TypeUint8
			fd.Size = 1

		case reflect.Uint16:
			fd.Type = TypeUint16
			fd.Size = 2

		case reflect.Uint32:
			fd.Type = TypeUint32
			fd.Size = 4

		case reflect.Uint64:
			fd.Type = TypeUint64
			fd.Size = 8

		case reflect.Int64:
			fd.Type = TypeInt64
			fd.Size = 8

		case reflect.Float64:
			fd.Type = TypeFloat64
			fd.Size = 8

		case reflect.Bool:
			fd.Type = TypeBool
			fd.Size = 1

		case reflect.Array:
			if sf.Type.Elem().Kind() != reflect.Uint8 {
				return nil, fmt.Errorf("statelog: field %q: only [N]byte arrays are supported, got [%d]%s", sf.Name, sf.Type.Len(), sf.Type.Elem().Kind())
			}
			fd.Type = TypeBytes
			fd.Size = uint16(sf.Type.Len())

		default:
			return nil, fmt.Errorf("statelog: field %q: unsupported type %s", sf.Name, sf.Type.Kind())
		}

		offset += fd.Size
		fields = append(fields, fd)
	}

	if len(fields) == 0 {
		return nil, fmt.Errorf("statelog: schema struct has no exported fields")
	}

	dataSize := uint32(offset)
	recordSize := dataSize + crc32Size
	// Pad to 8-byte alignment.
	if remainder := recordSize % 8; remainder != 0 {
		recordSize += 8 - remainder
	}

	byName := make(map[string]int, len(fields))
	for i, f := range fields {
		byName[f.Name] = i
	}

	return &Schema{
		Fields:       fields,
		RecordSize:   recordSize,
		DataSize:     dataSize,
		fieldsByName: byName,
	}, nil
}

// encodeSchema serializes the schema fields into a binary representation
// suitable for embedding in the file header.
func encodeSchema(s *Schema) []byte {
	// Estimate size: for each field, 1 (name len) + name + 1 (type) + 2 (size).
	size := 0
	for _, f := range s.Fields {
		size += 1 + len(f.Name) + 1 + 2
	}
	buf := make([]byte, 0, size)

	for _, f := range s.Fields {
		buf = append(buf, byte(len(f.Name)))
		buf = append(buf, []byte(f.Name)...)
		buf = append(buf, f.Type)
		buf = append(buf, byte(f.Size), byte(f.Size>>8))
	}

	return buf
}

// decodeSchema reads field definitions from buf and returns the reconstructed
// Schema along with the number of bytes consumed.
func decodeSchema(buf []byte, fieldCount int) (*Schema, int, error) {
	fields := make([]FieldDef, 0, fieldCount)
	var offset uint16
	pos := 0

	for i := 0; i < fieldCount; i++ {
		if pos >= len(buf) {
			return nil, pos, fmt.Errorf("statelog: schema truncated at field %d", i)
		}

		nameLen := int(buf[pos])
		pos++
		if pos+nameLen+3 > len(buf) {
			return nil, pos, fmt.Errorf("statelog: schema truncated reading field %d name", i)
		}

		name := string(buf[pos : pos+nameLen])
		pos += nameLen

		fieldType := buf[pos]
		pos++

		size := uint16(buf[pos]) | uint16(buf[pos+1])<<8
		pos += 2

		fields = append(fields, FieldDef{
			Name:   name,
			Type:   fieldType,
			Size:   size,
			Offset: offset,
		})
		offset += size
	}

	dataSize := uint32(offset)
	recordSize := dataSize + crc32Size
	if remainder := recordSize % 8; remainder != 0 {
		recordSize += 8 - remainder
	}

	byName := make(map[string]int, len(fields))
	for i, f := range fields {
		byName[f.Name] = i
	}

	return &Schema{
		Fields:       fields,
		RecordSize:   recordSize,
		DataSize:     dataSize,
		fieldsByName: byName,
	}, pos, nil
}
