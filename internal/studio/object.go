package studio

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

// object is a JSON object that preserves key order. Registry automation compares
// documents byte for byte, so entities must never pass through a Go map.
type object struct{ fields []field }

func parseObject(raw json.RawMessage) (*object, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := token.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("entity must be a JSON object")
	}
	result := &object{}
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
		result.set(key, value)
	}
	if _, err = decoder.Token(); err != nil {
		return nil, err
	}
	return result, nil
}

func (o *object) index(key string) int {
	for i := range o.fields {
		if o.fields[i].key == key {
			return i
		}
	}
	return -1
}

func (o *object) get(key string) (json.RawMessage, bool) {
	if i := o.index(key); i >= 0 {
		return o.fields[i].value, true
	}
	return nil, false
}

func (o *object) set(key string, value json.RawMessage) {
	if i := o.index(key); i >= 0 {
		o.fields[i].value = value
		return
	}
	o.fields = append(o.fields, field{key, value})
}

func (o *object) remove(key string) {
	if i := o.index(key); i >= 0 {
		o.fields = append(o.fields[:i], o.fields[i+1:]...)
	}
}

// moveLast keeps the listed keys, where present, at the end in the given order.
func (o *object) moveLast(keys ...string) {
	for _, key := range keys {
		if i := o.index(key); i >= 0 {
			moved := o.fields[i]
			o.fields = append(append(o.fields[:i:i], o.fields[i+1:]...), moved)
		}
	}
}

func (o *object) marshal() (json.RawMessage, error) {
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

// sameJSON compares two values without regard to whitespace.
func sameJSON(a, b json.RawMessage) bool {
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	var left, right bytes.Buffer
	if json.Compact(&left, a) != nil || json.Compact(&right, b) != nil {
		return bytes.Equal(a, b)
	}
	return bytes.Equal(left.Bytes(), right.Bytes())
}
