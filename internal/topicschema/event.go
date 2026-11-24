package topicschema

import (
	"bytes"
	"encoding/json"
)

// buildEvent precomputes the events/list entry of an event from its compact
// schemas; meta is nil when absent.
func buildEvent(def Definition, input, payload json.RawMessage) (*Event, error) {
	ev := &Event{Name: def.Name, Description: def.Description}
	quotedName, err := marshalNoEscape(def.Name)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString(`{"name":`)
	buf.Write(quotedName)
	if def.Description != "" {
		d, err := marshalNoEscape(def.Description)
		if err != nil {
			return nil, err
		}
		buf.WriteString(`,"description":`)
		buf.Write(d)
	}
	buf.WriteString(`,"delivery":["webhook"],"inputSchema":`)
	inputStart := buf.Len()
	buf.Write(input)
	inputEnd := buf.Len()
	buf.WriteString(`,"payloadSchema":`)
	payloadStart := buf.Len()
	buf.Write(payload)
	payloadEnd := buf.Len()
	if len(def.Meta) > 0 {
		buf.WriteString(`,"_meta":`)
		buf.Write(def.Meta)
	}
	buf.WriteByte('}')

	// InputSchema and PayloadSchema share JSON's memory rather than holding
	// another copy of each schema. The capacity limits keep an append to one
	// from writing into JSON.
	ev.JSON = bytes.Clone(buf.Bytes())
	ev.InputSchema = ev.JSON[inputStart:inputEnd:inputEnd]
	ev.PayloadSchema = ev.JSON[payloadStart:payloadEnd:payloadEnd]
	return ev, nil
}
