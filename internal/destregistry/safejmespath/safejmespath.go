// Package safejmespath runs JMESPath searches without letting a panic from
// the library escape. Some functions in go-jmespath type-assert their
// arguments or compare them with ==, so an expression that compiles can still
// panic on particular data (merge() over an array, contains() with an object).
package safejmespath

import (
	"errors"
	"fmt"

	"github.com/jmespath/go-jmespath"
)

var ErrPanic = errors.New("expression panicked during evaluation")

func Search(expression string, data interface{}) (result interface{}, err error) {
	defer recoverPanic(&err)
	return jmespath.Search(expression, data)
}

func SearchCompiled(compiled *jmespath.JMESPath, data interface{}) (result interface{}, err error) {
	defer recoverPanic(&err)
	return compiled.Search(data)
}

func recoverPanic(err *error) {
	if r := recover(); r != nil {
		*err = fmt.Errorf("%w: %v", ErrPanic, r)
	}
}

type TemplateError struct {
	Field string
	Err   error
}

func (e *TemplateError) Error() string {
	return fmt.Sprintf("%s could not be evaluated: %v", e.Field, e.Err)
}

func (e *TemplateError) Unwrap() error { return e.Err }

func (e *TemplateError) Message() string {
	if errors.Is(e.Err, ErrPanic) {
		return fmt.Sprintf("%s could not be evaluated: the expression failed on an unsupported value", e.Field)
	}
	return e.Error()
}

func MessageFor(err error) string {
	var te *TemplateError
	if errors.As(err, &te) {
		return te.Message()
	}
	return ""
}
