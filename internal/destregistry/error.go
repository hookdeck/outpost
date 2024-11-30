package destregistry

import "fmt"

func NewErrDestinationValidation(err error) error {
	return fmt.Errorf("validation failed: %w", err)
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
