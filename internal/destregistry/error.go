package destregistry

import "fmt"

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

type ErrDestinationPublish struct {
	Err error
}

var _ error = &ErrDestinationPublish{}

func (e *ErrDestinationPublish) Error() string {
	return e.Err.Error()
}

func NewErrDestinationPublish(err error) error {
	return &ErrDestinationPublish{Err: err}
}
