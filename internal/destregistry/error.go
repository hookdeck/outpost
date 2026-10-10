package destregistry

import (
	"context"
	"errors"
	"fmt"
)

type ErrDestinationValidation struct {
	Errors []ValidationErrorDetail `json:"errors"`
	// Cause optionally carries a provider-specific error (e.g. a protocol
	// error with its own wire shape) for callers that know how to render it.
	// It never appears in the JSON form or in Error().
	Cause error `json:"-"`
}

type ValidationErrorDetail struct {
	Field string `json:"field"`
	Type  string `json:"type"`
}

func (e *ErrDestinationValidation) Error() string {
	return fmt.Sprintf("validation failed")
}

// Unwrap exposes Cause to errors.Is/errors.As.
func (e *ErrDestinationValidation) Unwrap() error {
	return e.Cause
}

func NewErrDestinationValidation(errors []ValidationErrorDetail) error {
	return &ErrDestinationValidation{Errors: errors}
}

// ErrDestinationPublishAttempt is a failed delivery attempt. It deliberately has
// no Unwrap: the registry classifies publisher errors with errors.Is (canceled,
// deadline exceeded), and unwrapping would let a wrapped sentinel reclassify a
// failed attempt.
type ErrDestinationPublishAttempt struct {
	Err      error
	Provider string
	Data     map[string]interface{}
	// NonRetryable marks a failure that retrying cannot fix (e.g. the receiver
	// answered 410 Gone or 413, or the payload is over the provider's size
	// limit). No automatic retry is scheduled for it.
	NonRetryable bool
}

var _ error = &ErrDestinationPublishAttempt{}

func (e *ErrDestinationPublishAttempt) Error() string {
	return fmt.Sprintf("failed to publish to %s: %v", e.Provider, e.Err)
}

func NewErrDestinationPublishAttempt(err error, provider string, data map[string]interface{}) error {
	return &ErrDestinationPublishAttempt{Err: err, Provider: provider, Data: data}
}

// IsNonRetryable reports whether err carries an *ErrDestinationPublishAttempt
// marked NonRetryable.
func IsNonRetryable(err error) bool {
	var pubErr *ErrDestinationPublishAttempt
	return errors.As(err, &pubErr) && pubErr.NonRetryable
}

// NewFormatError returns the (*Delivery, error) a publisher should return when
// formatting an event fails before it can be sent (e.g. an invalid key/partition
// template or an unparseable payload). It records a failed attempt so the failure
// is visible to the customer and the message is acked, instead of nacking into the DLQ.
//
// message is the customer-facing string persisted on the attempt (ResponseData);
// when empty a generic default is used. The raw err is carried only in the returned
// error (for logs/telemetry) and is not persisted on the attempt.
func NewFormatError(provider, message string, err error) (*Delivery, error) {
	if message == "" {
		message = "could not format event for delivery"
	}
	return &Delivery{
		Status:   "failed",
		Code:     "ERR",
		Response: map[string]interface{}{"error": message},
	}, NewErrDestinationPublishAttempt(err, provider, map[string]interface{}{
		"error": "format_failed",
	})
}

// NewErrPublishCanceled creates an error for when publish is canceled (e.g., service shutdown).
// This should return nil Delivery to trigger nack → requeue for another instance.
// See: https://github.com/hookdeck/outpost/issues/571
func NewErrPublishCanceled(provider string) error {
	return &ErrDestinationPublishAttempt{
		Err:      context.Canceled,
		Provider: provider,
		Data:     map[string]interface{}{"error": "canceled"},
	}
}

var ErrPublisherClosed = errors.New("publisher is closed")
