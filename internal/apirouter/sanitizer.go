package apirouter

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

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

	var requestData map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &requestData); err != nil {
		return unparseableBody(bodyBytes), nil
	}

	sanitizedBytes, err := json.Marshal(s.sanitizeRequest(requestData))
	if err != nil {
		return unparseableBody(bodyBytes), nil
	}

	return sanitizedBytes, nil
}

func unparseableBody(body []byte) []byte {
	return []byte(fmt.Sprintf("[REQUEST_BODY_UNPARSEABLE: %d bytes]", len(body)))
}

// sanitizeRequest redacts credentials and sensitive config fields in a request
// payload. credentials, type and config are matched case-insensitively, as
// encoding/json does when the handlers decode the same body. Keys inside
// config are matched the same way, which is broader than the decoder.
func (s *RequestBodySanitizer) sanitizeRequest(data map[string]interface{}) map[string]interface{} {
	sanitized := make(map[string]interface{}, len(data))
	var configFields []metadata.FieldSchema

	for k, v := range data {
		switch {
		case strings.EqualFold(k, "credentials"):
			sanitized[k] = redactCredentials(v)
		case strings.EqualFold(k, "type"):
			sanitized[k] = v
			if destinationType, ok := v.(string); ok {
				if meta, err := s.registry.MetadataLoader().Load(destinationType); err == nil {
					configFields = append(configFields, meta.ConfigFields...)
				}
			}
		default:
			sanitized[k] = v
		}
	}

	for k, v := range sanitized {
		if !strings.EqualFold(k, "config") {
			continue
		}
		if config, ok := v.(map[string]interface{}); ok {
			sanitized[k] = s.sanitizeFieldsMap(config, configFields)
		}
	}

	return sanitized
}

func redactCredentials(value interface{}) interface{} {
	if value == nil {
		return nil
	}
	credentials, ok := value.(map[string]interface{})
	if !ok {
		return SensitiveFieldMask
	}
	redacted := make(map[string]interface{}, len(credentials))
	for k := range credentials {
		redacted[k] = SensitiveFieldMask
	}
	return redacted
}

// sanitizeFieldsMap sanitizes a map based on field schemas
func (s *RequestBodySanitizer) sanitizeFieldsMap(fields map[string]interface{}, schemas []metadata.FieldSchema) map[string]interface{} {
	sanitized := make(map[string]interface{}, len(fields))

	for k, v := range fields {
		sanitized[k] = v
		for _, schema := range schemas {
			if schema.Sensitive && strings.EqualFold(schema.Key, k) {
				sanitized[k] = SensitiveFieldMask
				break
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
