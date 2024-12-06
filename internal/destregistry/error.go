package destregistry

import (
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
	Data     interface{}
}

var _ error = &ErrDestinationPublishAttempt{}

func (e *ErrDestinationPublishAttempt) Error() string {
	return fmt.Sprintf("failed to publish to %s: %v", e.Provider, e.Err)
}

func NewErrDestinationPublishAttempt(err error, provider string, data interface{}) error {
	return &ErrDestinationPublishAttempt{Err: err, Provider: provider, Data: data}
}

var ErrPublisherClosed = errors.New("publisher is closed")

type ErrUnexpectedPublishError struct {
	Err error
}

var _ error = &ErrUnexpectedPublishError{}

func (e *ErrUnexpectedPublishError) Error() string {
	return fmt.Sprintf("unexpected publish error: %v", e.Err)
}
