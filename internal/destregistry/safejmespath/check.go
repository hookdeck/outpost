package safejmespath

import (
	"errors"
	"reflect"

	"github.com/jmespath/go-jmespath"
)

// MaxReferences is the largest number of field paths a template may contain.
// A path inside to_string counts toStringWeight times.
const MaxReferences = 32

// toStringWeight is how much to_string can enlarge a value: JSON-encoding an
// object or array writes some characters as six-byte escapes.
const toStringWeight = 6

// ErrNotAccepted marks a template that parses but is outside the accepted
// forms. A template that does not parse yields the parser's error instead.
var ErrNotAccepted = errors.New("template is outside the accepted forms")

// Check reports whether expression is a template of an accepted form:
//
//	template = scalar
//	scalar   = path | literal | to_string(scalar) | not_null(scalar, ...)
//	         | scalar || scalar | join('<literal>', [scalar, ...])
//	path     = field(.field)* with optional [integer] steps
//	literal  = 'string' or a JSON string, number, boolean or null
//
// go-jmespath cannot be interrupted and has no limits, so templates are held
// to forms whose result stays proportional to the input. A field path returns
// at most the whole input, to_string enlarges it at most toStringWeight
// times, and join adds its literal separator once per item. With paths
// counted against MaxReferences, the result and every intermediate value are
// bounded by that many copies of the input plus what the template's literals
// add, and evaluation time is linear in the input.
func Check(expression string) (err error) {
	defer func() {
		if recover() != nil {
			err = ErrNotAccepted
		}
	}()
	root, err := jmespath.NewParser().Parse(expression)
	if err != nil {
		return err
	}
	c := checker{}
	if !c.scalar(astNode{reflect.ValueOf(root)}) || c.refs > MaxReferences {
		return ErrNotAccepted
	}
	return nil
}

// Compile is Check followed by jmespath.Compile.
func Compile(expression string) (*jmespath.JMESPath, error) {
	if err := Check(expression); err != nil {
		return nil, err
	}
	return jmespath.Compile(expression)
}

// Node type numbers of go-jmespath v0.4.0, which keeps them unexported. A test
// pins them against the library.
const (
	kindEmpty              = 0
	kindComparator         = 1
	kindCurrentNode        = 2
	kindExpRef             = 3
	kindFunctionExpression = 4
	kindField              = 5
	kindFilterProjection   = 6
	kindFlatten            = 7
	kindIdentity           = 8
	kindIndex              = 9
	kindIndexExpression    = 10
	kindKeyValPair         = 11
	kindLiteral            = 12
	kindMultiSelectHash    = 13
	kindMultiSelectList    = 14
	kindOrExpression       = 15
	kindAndExpression      = 16
	kindNotExpression      = 17
	kindPipe               = 18
	kindProjection         = 19
	kindSubexpression      = 20
	kindSlice              = 21
	kindValueProjection    = 22
)

// astNode reads a jmespath.ASTNode through reflection, because its fields are
// unexported.
type astNode struct{ v reflect.Value }

func (n astNode) kind() int { return int(n.v.FieldByName("nodeType").Int()) }

func (n astNode) children() []astNode {
	c := n.v.FieldByName("children")
	out := make([]astNode, c.Len())
	for i := range out {
		out[i] = astNode{c.Index(i)}
	}
	return out
}

// value is the node's value, or an invalid Value when it has none (or null).
func (n astNode) value() reflect.Value { return n.v.FieldByName("value").Elem() }

func (n astNode) stringValue() (string, bool) {
	v := n.value()
	if !v.IsValid() || v.Kind() != reflect.String {
		return "", false
	}
	return v.String(), true
}

type checker struct{ refs int }

// with returns the children of n when n has the given kind and child count.
func with(n astNode, kind, count int) ([]astNode, bool) {
	if n.kind() != kind {
		return nil, false
	}
	c := n.children()
	return c, len(c) == count
}

func (c *checker) scalar(n astNode) bool {
	switch n.kind() {
	case kindField, kindSubexpression, kindIndexExpression:
		c.refs++
		return path(n)
	case kindLiteral:
		v := n.value()
		if !v.IsValid() {
			return true // null
		}
		switch v.Kind() {
		case reflect.String, reflect.Float64, reflect.Bool:
			return true
		}
		return false
	case kindOrExpression:
		kids, ok := with(n, kindOrExpression, 2)
		return ok && c.scalar(kids[0]) && c.scalar(kids[1])
	case kindFunctionExpression:
		return c.function(n)
	}
	return false
}

func (c *checker) function(n astNode) bool {
	name, ok := n.stringValue()
	if !ok {
		return false
	}
	args := n.children()
	switch name {
	case "to_string":
		before := c.refs
		ok := len(args) == 1 && c.scalar(args[0])
		c.refs += (toStringWeight - 1) * (c.refs - before)
		return ok
	case "not_null":
		if len(args) == 0 {
			return false
		}
		for _, a := range args {
			if !c.scalar(a) {
				return false
			}
		}
		return true
	case "join":
		if len(args) != 2 || args[0].kind() != kindLiteral {
			return false
		}
		if v := args[0].value(); !v.IsValid() || v.Kind() != reflect.String {
			return false
		}
		if args[1].kind() != kindMultiSelectList {
			return false
		}
		items := args[1].children()
		if len(items) == 0 {
			return false
		}
		for _, item := range items {
			if !c.scalar(item) {
				return false
			}
		}
		return true
	}
	return false
}

// path accepts field, path.field and path[integer] (also path.field[integer]).
func path(n astNode) bool {
	switch n.kind() {
	case kindField:
		_, ok := n.stringValue()
		return ok && len(n.children()) == 0
	case kindSubexpression:
		kids, ok := with(n, kindSubexpression, 2)
		return ok && path(kids[0]) && step(kids[1])
	case kindIndexExpression:
		return indexed(n, path)
	}
	return false
}

// step is the right side of a dot: a field with optional [integer] steps.
func step(n astNode) bool {
	switch n.kind() {
	case kindField:
		return path(n)
	case kindIndexExpression:
		return indexed(n, step)
	}
	return false
}

func indexed(n astNode, base func(astNode) bool) bool {
	kids, ok := with(n, kindIndexExpression, 2)
	if !ok || !base(kids[0]) || kids[1].kind() != kindIndex || len(kids[1].children()) != 0 {
		return false
	}
	v := kids[1].value()
	return v.IsValid() && v.Kind() == reflect.Int
}
