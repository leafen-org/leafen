package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// A tool's schema is listed with its keys in the order it was written in —
// the order of its Go fields — after mcpPlainToolSchemas wrote it again; read
// into Go maps, every object's keys came out sorted.

// schemaObject is a JSON object of a schema with its keys in their order.
type schemaObject struct {
	keys   []string
	values map[string]any
}

func newSchemaObject() *schemaObject {
	return &schemaObject{values: map[string]any{}}
}

func (object *schemaObject) set(key string, value any) {
	if _, ok := object.values[key]; !ok {
		object.keys = append(object.keys, key)
	}
	object.values[key] = value
}

func (object *schemaObject) remove(key string) {
	if _, ok := object.values[key]; !ok {
		return
	}
	delete(object.values, key)
	for index, each := range object.keys {
		if each == key {
			object.keys = append(object.keys[:index:index], object.keys[index+1:]...)
			return
		}
	}
}

func (object *schemaObject) MarshalJSON() ([]byte, error) {
	var out bytes.Buffer
	out.WriteByte('{')
	for index, key := range object.keys {
		if index > 0 {
			out.WriteByte(',')
		}
		name, err := json.Marshal(key)
		if err != nil {
			return nil, err
		}
		value, err := json.Marshal(object.values[key])
		if err != nil {
			return nil, err
		}
		out.Write(name)
		out.WriteByte(':')
		out.Write(value)
	}
	out.WriteByte('}')
	return out.Bytes(), nil
}

// decodeSchemaInOrder reads a schema's JSON with its objects' keys in order
// and its numbers as written.
func decodeSchemaInOrder(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	value, err := decodeSchemaValue(decoder)
	if err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("schema: more after its value")
	}
	return value, nil
}

func decodeSchemaValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	switch token {
	case json.Delim('{'):
		object := newSchemaObject()
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, errors.New("schema: an object key that is not a string")
			}
			value, err := decodeSchemaValue(decoder)
			if err != nil {
				return nil, err
			}
			object.set(key, value)
		}
		_, err := decoder.Token()
		return object, err
	case json.Delim('['):
		list := []any{}
		for decoder.More() {
			value, err := decodeSchemaValue(decoder)
			if err != nil {
				return nil, err
			}
			list = append(list, value)
		}
		_, err := decoder.Token()
		return list, err
	}
	return token, nil
}
