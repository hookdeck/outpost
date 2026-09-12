package destawseventbridge

import (
	"errors"
	"testing"

	smithy "github.com/aws/smithy-go"
	"github.com/stretchr/testify/assert"
)

// smithyAPIError is a minimal smithy.APIError implementation for tests, since
// the real error types live deep in generated SDK code.
type smithyAPIError struct {
	code    string
	message string
}

func (e *smithyAPIError) Error() string                 { return e.code + ": " + e.message }
func (e *smithyAPIError) ErrorCode() string             { return e.code }
func (e *smithyAPIError) ErrorMessage() string          { return e.message }
func (e *smithyAPIError) ErrorFault() smithy.ErrorFault { return smithy.FaultUnknown }

var _ smithy.APIError = (*smithyAPIError)(nil)

func TestClassifyErrorCode_NeverEchoesAWSMessage(t *testing.T) {
	t.Parallel()

	// AccessDeniedException-style messages routinely embed account IDs and
	// ARNs, e.g. "User: arn:aws:iam::123456789012:user/foo is not authorized
	// to perform: events:PutEvents on resource: arn:aws:events:us-east-1:
	// 123456789012:event-bus/prod". None of that must ever appear in what we
	// hand back to a caller.
	sensitiveAWSMessage := "User: arn:aws:iam::123456789012:user/foo is not authorized to perform: events:PutEvents on resource: arn:aws:events:us-east-1:123456789012:event-bus/prod"

	tests := []struct {
		name      string
		errorCode string
	}{
		{"access denied", "AccessDeniedException"},
		{"not authorized for source", "NotAuthorizedForSourceException"},
		{"not authorized for detail type", "NotAuthorizedForDetailTypeException"},
		{"invalid account", "InvalidAccountIdException"},
		{"invalid argument", "InvalidArgument"},
		{"malformed detail", "MalformedDetail"},
		{"throttling", "ThrottlingException"},
		{"internal failure", "InternalFailure"},
		{"redaction failure", "RedactionFailure"},
		{"unrecognized code", "SomeFutureErrorCodeNotYetHandled"},
		{"empty code", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			code, message := classifyErrorCode(tt.errorCode)

			assert.NotContains(t, message, "arn:aws", "sanitized message must never contain an ARN")
			assert.NotContains(t, message, "123456789012", "sanitized message must never contain an account ID")
			assert.NotEqual(t, sensitiveAWSMessage, message, "sanitized message must never be AWS's own ErrorMessage")
			assert.NotEmpty(t, code)
			assert.NotEmpty(t, message)
		})
	}
}

func TestClassifyError_TopLevel(t *testing.T) {
	t.Parallel()

	t.Run("smithy API error is classified by its error code", func(t *testing.T) {
		t.Parallel()
		err := &smithyAPIError{code: "ThrottlingException", message: "rate exceeded for account 123456789012"}
		code, message := classifyError(err)
		assert.Equal(t, "ThrottlingException", code)
		assert.NotContains(t, message, "123456789012")
	})

	t.Run("a plain non-API error falls back to a generic code", func(t *testing.T) {
		t.Parallel()
		err := errors.New("dial tcp: connection refused")
		code, message := classifyError(err)
		assert.Equal(t, "request_failed", code)
		assert.NotContains(t, message, "dial tcp")
	})
}
