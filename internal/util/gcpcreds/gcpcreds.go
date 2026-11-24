// Package gcpcreds validates GCP credential JSON before it is handed to
// Google's auth libraries.
package gcpcreds

import (
	"encoding/json"
	"errors"
)

const serviceAccountType = "service_account"

var (
	ErrInvalidJSON       = errors.New("gcp credentials: invalid JSON")
	ErrMissingType       = errors.New("gcp credentials: missing type")
	ErrNotServiceAccount = errors.New("gcp credentials: not a service account key")
)

// CheckServiceAccount returns nil if data is a service account key. It reads
// the type the way Google's auth libraries do: decoding into a struct, so the
// "type" key matches case-insensitively and the last match wins.
func CheckServiceAccount(data []byte) error {
	var f struct {
		Type *string `json:"type"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return ErrInvalidJSON
	}
	if f.Type == nil {
		return ErrMissingType
	}
	if *f.Type != serviceAccountType {
		return ErrNotServiceAccount
	}
	return nil
}
