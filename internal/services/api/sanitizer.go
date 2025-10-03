package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/hookdeck/outpost/internal/destregistry"
	"github.com/hookdeck/outpost/internal/destregistry/metadata"
)

const (
	// MaxRequestBodySize limits the size of request bodies we'll buffer for logging
	// Set to 10KB to prevent excessive log volumes
	MaxRequestBodySize = 10 * 1024
	// SensitiveFieldMask is used to replace sensitive values
	SensitiveFieldMask = "[REDACTED]"
)

// RequestBodySanitizer handles sanitization of request bodies for logging
type RequestBodySanitizer struct {
	registry destregistry.Registry
}

// NewRequestBodySanitizer creates a new sanitizer instance
func NewRequestBodySanitizer(registry destregistry.Registry) *RequestBodySanitizer {
	return &RequestBodySanitizer{
		registry: registry,
	}
}

// SanitizeRequestBody reads and sanitizes a request body for safe logging
func (s *RequestBodySanitizer) SanitizeRequestBody(body io.Reader) ([]byte, error) {
	// Read the body with size limit
	limitedReader := io.LimitReader(body, MaxRequestBodySize+1)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	// Check if body exceeds size limit
	if len(bodyBytes) > MaxRequestBodySize {
		return []byte(fmt.Sprintf("[REQUEST_BODY_TOO_LARGE: >%d bytes]", MaxRequestBodySize)), nil
	}

	if len(bodyBytes) == 0 {
		return []byte{}, nil
	}

	// Try to parse as JSON
	var requestData map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &requestData); err != nil {
		// If not valid JSON, return as-is (might be form data or other format)
		return bodyBytes, nil
	}

	// Sanitize the parsed JSON
	sanitizedData := s.sanitizeDestinationRequest(requestData)

	// Marshal back to JSON
	sanitizedBytes, err := json.Marshal(sanitizedData)
	if err != nil {
		return bodyBytes, nil // Return original if we can't marshal back
	}

	return sanitizedBytes, nil
}

// sanitizeDestinationRequest sanitizes a destination request payload
func (s *RequestBodySanitizer) sanitizeDestinationRequest(data map[string]interface{}) map[string]interface{} {
	// Make a copy to avoid modifying the original
	sanitized := make(map[string]interface{})
	for k, v := range data {
		sanitized[k] = v
	}

	// Get the destination type to load metadata
	destinationType, ok := sanitized["type"].(string)
	if !ok {
		return sanitized // Can't determine type, return as-is
	}

	// Load metadata for this destination type
	meta, err := s.registry.MetadataLoader().Load(destinationType)
	if err != nil {
		return sanitized // If we can't load metadata, return as-is
	}

	// Sanitize credentials field based on metadata
	if credentials, ok := sanitized["credentials"].(map[string]interface{}); ok {
		sanitized["credentials"] = s.sanitizeFieldsMap(credentials, meta.CredentialFields)
	}

	// Also check config fields for any marked as sensitive
	if config, ok := sanitized["config"].(map[string]interface{}); ok {
		sanitized["config"] = s.sanitizeFieldsMap(config, meta.ConfigFields)
	}

	return sanitized
}

// sanitizeFieldsMap sanitizes a map based on field schemas
func (s *RequestBodySanitizer) sanitizeFieldsMap(fields map[string]interface{}, schemas []metadata.FieldSchema) map[string]interface{} {
	sanitized := make(map[string]interface{})

	// Copy all fields first
	for k, v := range fields {
		sanitized[k] = v
	}

	// Find sensitive fields and redact them
	for _, schema := range schemas {
		if schema.Sensitive {
			if _, exists := sanitized[schema.Key]; exists {
				sanitized[schema.Key] = SensitiveFieldMask
			}
		}
	}

	return sanitized
}

// BufferedReader creates a reader that can be used multiple times
type BufferedReader struct {
	buffer []byte
}

// NewBufferedReader creates a new BufferedReader from an io.Reader
func NewBufferedReader(r io.Reader) (*BufferedReader, error) {
	if r == nil {
		return &BufferedReader{buffer: []byte{}}, nil
	}

	limitedReader := io.LimitReader(r, MaxRequestBodySize+1)
	buffer, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, err
	}

	return &BufferedReader{buffer: buffer}, nil
}

// Read implements io.Reader
func (br *BufferedReader) Read(p []byte) (n int, err error) {
	if len(br.buffer) == 0 {
		return 0, io.EOF
	}

	n = copy(p, br.buffer)
	br.buffer = br.buffer[n:]

	if len(br.buffer) == 0 {
		err = io.EOF
	}

	return n, err
}

// Bytes returns a copy of the buffered content
func (br *BufferedReader) Bytes() []byte {
	return append([]byte(nil), br.buffer...)
}

// NewReader creates a new reader from the buffered content
func (br *BufferedReader) NewReader() io.Reader {
	return bytes.NewReader(br.buffer)
}

// NewReadCloser creates a new ReadCloser from the buffered content
type nopCloser struct {
	io.Reader
}

func (nopCloser) Close() error { return nil }

func (br *BufferedReader) NewReadCloser() io.ReadCloser {
	return nopCloser{bytes.NewReader(br.buffer)}
}
