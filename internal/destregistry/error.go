package destregistry

import (
	"context"
	"errors"
	"fmt"
)

type ErrDestinationValidation struct {
	Errors []ValidationErrorDetail `json:"errors"`
}

type ValidationErrorDetail struct {
	Field string `json:"field"`
	Type  string `json:"type"`
}

func (e *ErrDestinationValidation) Error() string {
	return fmt.Sprintf("validation failed")
}

func NewErrDestinationValidation(errors []ValidationErrorDetail) error {
	return &ErrDestinationValidation{Errors: errors}
}

type ErrDestinationPublishAttempt struct {
	Err      error
	Provider string
	Data     map[string]interface{}
}

var _ error = &ErrDestinationPublishAttempt{}

func (e *ErrDestinationPublishAttempt) Error() string {
	return fmt.Sprintf("failed to publish to %s: %v", e.Provider, e.Err)
}

func NewErrDestinationPublishAttempt(err error, provider string, data map[string]interface{}) error {
	return &ErrDestinationPublishAttempt{Err: err, Provider: provider, Data: data}
}

// NewFormatErrorDelivery returns the (*Delivery, error) a publisher should return when
// formatting an event fails before it can be sent (e.g. an invalid key/partition
// template or an unparseable payload). It records a failed attempt so the failure
// is visible to the customer and the message is acked, instead of nacking into the DLQ.
//
// message is the customer-facing string persisted on the attempt (ResponseData);
// when empty a generic default is used. The raw err is carried only in the returned
// error (for logs/telemetry) and is not persisted on the attempt.
func NewFormatErrorDelivery(provider, message string, err error) (*Delivery, error) {
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
