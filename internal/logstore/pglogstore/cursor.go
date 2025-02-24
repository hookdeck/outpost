package pglogstore

import (
	"fmt"
	"math/big"
)

type cursorEncoder interface {
	Encode(raw string) string
	Decode(encoded string) (string, error)
}

type base62CursorEncoder struct{}

func (e *base62CursorEncoder) Encode(raw string) string {
	num := new(big.Int)
	num.SetBytes([]byte(raw))
	return num.Text(62)
}

func (e *base62CursorEncoder) Decode(encoded string) (string, error) {
	num := new(big.Int)
	num, ok := num.SetString(encoded, 62)
	if !ok {
		return "", fmt.Errorf("invalid cursor encoding")
	}
	return string(num.Bytes()), nil
}

type eventCursorParser struct {
	encoder cursorEncoder
}

func newEventCursorParser() eventCursorParser {
	return eventCursorParser{
		encoder: &base62CursorEncoder{},
	}
}

func (p *eventCursorParser) Parse(cursor string) (string, error) {
	return p.encoder.Decode(cursor)
}

func (p *eventCursorParser) Format(timeID string) string {
	return p.encoder.Encode(timeID)
}
