// Package jsonobject edits JSON objects without disturbing key order or the
// bytes of untouched values. Registry automation compares documents byte for
// byte, so entities must never pass through a Go map.
package jsonobject

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// field is one key of a JSON object whose value is kept byte-exact.
type field struct {
	key   string
	value json.RawMessage
}

// Object is a JSON object that preserves key order.
type Object struct{ fields []field }

// Parse decodes one JSON object.
func Parse(raw json.RawMessage) (*Object, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("entity must be a JSON object")
	}
	result := &Object{}
	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		if tokenErr != nil {
			return nil, tokenErr
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("invalid object key")
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return nil, err
		}
		result.Set(key, value)
	}
	if _, err = decoder.Token(); err != nil {
		return nil, err
	}
	return result, nil
}

func (o *Object) index(key string) int {
	for i := range o.fields {
		if o.fields[i].key == key {
			return i
		}
	}
	return -1
}

// Get returns the raw value of key.
func (o *Object) Get(key string) (json.RawMessage, bool) {
	if i := o.index(key); i >= 0 {
		return o.fields[i].value, true
	}
	return nil, false
}

// Set replaces key in place, or appends it when absent.
func (o *Object) Set(key string, value json.RawMessage) {
	if i := o.index(key); i >= 0 {
		o.fields[i].value = value
		return
	}
	o.fields = append(o.fields, field{key, value})
}

// Remove deletes key if present.
func (o *Object) Remove(key string) {
	if i := o.index(key); i >= 0 {
		o.fields = append(o.fields[:i], o.fields[i+1:]...)
	}
}

// MoveLast keeps the listed keys, where present, at the end in the given order.
func (o *Object) MoveLast(keys ...string) {
	for _, key := range keys {
		if i := o.index(key); i >= 0 {
			moved := o.fields[i]
			o.fields = append(append(o.fields[:i:i], o.fields[i+1:]...), moved)
		}
	}
}

// Marshal renders the object compactly in its current key order.
func (o *Object) Marshal() (json.RawMessage, error) {
	var b bytes.Buffer
	b.WriteByte('{')
	for i := range o.fields {
		if i > 0 {
			b.WriteByte(',')
		}
		key, err := json.Marshal(o.fields[i].key)
		if err != nil {
			return nil, err
		}
		b.Write(key)
		b.WriteByte(':')
		if err = json.Compact(&b, o.fields[i].value); err != nil {
			return nil, err
		}
	}
	b.WriteByte('}')
	return b.Bytes(), nil
}
