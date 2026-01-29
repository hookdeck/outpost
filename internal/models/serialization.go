package models

import (
	"encoding"
	"encoding/json"
	"fmt"
	"log"
	"strings"
)

// ============================== Interface assertions ==============================

var _ encoding.BinaryMarshaler = &Topics{}
var _ encoding.BinaryUnmarshaler = &Topics{}
var _ json.Marshaler = &Topics{}
var _ json.Unmarshaler = &Topics{}

var _ encoding.BinaryMarshaler = &Filter{}
var _ encoding.BinaryUnmarshaler = &Filter{}

var _ encoding.BinaryMarshaler = &MapStringString{}
var _ encoding.BinaryUnmarshaler = &MapStringString{}
var _ json.Unmarshaler = &MapStringString{}

var _ fmt.Stringer = &Data{}
var _ encoding.BinaryUnmarshaler = &Data{}

// ============================== Topics serialization ==============================

func (t *Topics) MarshalBinary() ([]byte, error) {
	str := strings.Join(*t, ",")
	return []byte(str), nil
}

func (t *Topics) UnmarshalBinary(data []byte) error {
	*t = TopicsFromString(string(data))
	return nil
}

func (t *Topics) MarshalJSON() ([]byte, error) {
	return json.Marshal(*t)
}

func (t *Topics) UnmarshalJSON(data []byte) error {
	if string(data) == `"*"` {
		*t = TopicsFromString("*")
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		log.Println(err)
		return ErrInvalidTopicsFormat
	}
	*t = arr
	return nil
}

// ============================== Filter ==============================

// Filter represents a JSON schema filter for event matching.
// It uses the simplejsonmatch schema syntax for filtering events.
type Filter map[string]any

func (f *Filter) MarshalBinary() ([]byte, error) {
	if f == nil || len(*f) == 0 {
		return nil, nil
	}
	return json.Marshal(f)
}

func (f *Filter) UnmarshalBinary(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, f)
}

// ============================== MapStringString ==============================

type Config = MapStringString
type Credentials = MapStringString
type DeliveryMetadata = MapStringString
type MapStringString map[string]string

func (m *MapStringString) MarshalBinary() ([]byte, error) {
	return json.Marshal(m)
}

func (m *MapStringString) UnmarshalBinary(data []byte) error {
	return json.Unmarshal(data, m)
}

func (m *MapStringString) UnmarshalJSON(data []byte) error {
	// First try to unmarshal as map[string]string
	var stringMap map[string]string
	if err := json.Unmarshal(data, &stringMap); err == nil {
		*m = stringMap
		return nil
	}

	// If that fails, try map[string]interface{} to handle mixed types
	var mixedMap map[string]interface{}
	if err := json.Unmarshal(data, &mixedMap); err != nil {
		return err
	}

	// Convert all values to strings
	result := make(map[string]string)
	for k, v := range mixedMap {
		switch val := v.(type) {
		case string:
			result[k] = val
		case bool:
			result[k] = fmt.Sprintf("%v", val)
		case float64:
			result[k] = fmt.Sprintf("%v", val)
		case nil:
			result[k] = ""
		default:
			// For other types, try to convert to string using JSON marshaling
			if b, err := json.Marshal(val); err == nil {
				result[k] = string(b)
			} else {
				result[k] = fmt.Sprintf("%v", val)
			}
		}
	}

	*m = result
	return nil
}

// ============================== Data ==============================

type Data map[string]interface{}

func (d *Data) String() string {
	data, err := json.Marshal(d)
	if err != nil {
		return ""
	}
	return string(data)
}

func (d *Data) UnmarshalBinary(data []byte) error {
	if string(data) == "" {
		return nil
	}
	return json.Unmarshal(data, d)
}

// ============================== Metadata ==============================

type Metadata = MapStringString
