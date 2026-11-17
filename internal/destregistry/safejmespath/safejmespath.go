// Package safejmespath runs JMESPath searches without letting a panic from
// the library escape. Some functions in go-jmespath type-assert their
// arguments or compare them with ==, so an expression that compiles can still
// panic on particular data (merge() over an array, contains() with an object).
package safejmespath

import (
	"errors"
	"fmt"
	"runtime/debug"

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

// PanicError is the error for a recovered panic. It matches ErrPanic and
// keeps the panic value and stack for logging.
type PanicError struct {
	Value interface{}
	Stack []byte
}

func (e *PanicError) Error() string { return fmt.Sprintf("%v: %v", ErrPanic, e.Value) }

func (e *PanicError) Unwrap() error { return ErrPanic }

func recoverPanic(err *error) {
	if r := recover(); r != nil {
		*err = &PanicError{Value: r, Stack: debug.Stack()}
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

// Message is the text shown on the failed attempt. It leaves out the panic
// value, which names Go types.
func (e *TemplateError) Message() string {
	if errors.Is(e.Err, ErrPanic) {
		return fmt.Sprintf("%s could not be evaluated: the expression failed on an unsupported value", e.Field)
	}
	return e.Error()
}

// MessageFor returns the attempt message for a TemplateError in err's chain,
// or "" when there is none.
func MessageFor(err error) string {
	var te *TemplateError
	if errors.As(err, &te) {
		return te.Message()
	}
	return ""
}
