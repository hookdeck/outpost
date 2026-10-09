package topicschema

import (
	"fmt"
	"math/big"
	"slices"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// maxReportedErrors caps the messages rendered for one validation.
const maxReportedErrors = 20

// maxCountedErrors bounds the error nodes walked past maxReportedErrors to
// count the rest. Beyond it the summary reads "... and more".
const maxCountedErrors = 10000

// renderValidationErrors flattens a validation error tree into at most
// maxReportedErrors entries of the form "<path>: <message>", plus a trailing
// "... and N more" when some were left out.
//
// Entries never contain instance values. Each message is a fixed template per
// error kind filled with schema-side values only; the library's own messages
// quote instance values and are never used. Paths start at root and follow
// the instance in dot form (data.items[2].sku). Object keys appear only when
// names holds them, that is when the schema declares them; any other key,
// such as one matched by additionalProperties or patternProperties, renders
// as "*". A nil names shows every key, for schema configuration errors.
func renderValidationErrors(verr *jsonschema.ValidationError, root string, instance any, names map[string]struct{}) []string {
	r := errorRenderer{root: root, instance: instance, names: names, seen: map[string]bool{}}
	r.walk(verr)
	// The library visits object properties in map order; sorting keeps the
	// output stable for the same input.
	slices.Sort(r.out)
	switch {
	case r.truncated:
		r.out = append(r.out, "... and more")
	case r.more > 0:
		r.out = append(r.out, fmt.Sprintf("... and %d more", r.more))
	}
	return r.out
}

type errorRenderer struct {
	root     string
	instance any
	names    map[string]struct{}
	out      []string
	seen     map[string]bool
	// more counts the leaves left out once out is full, and visited the
	// nodes walked to count them.
	more      int
	visited   int
	truncated bool
}

// walk renders the leaves under e depth-first, in the library's order.
func (r *errorRenderer) walk(e *jsonschema.ValidationError) {
	if r.truncated {
		return
	}
	full := len(r.out) >= maxReportedErrors
	if full {
		r.visited++
		if r.visited > maxCountedErrors {
			r.truncated = true
			return
		}
	}
	if len(e.Causes) > 0 && !opaqueKind(e.ErrorKind) {
		for _, cause := range e.Causes {
			r.walk(cause)
		}
		return
	}
	if full {
		r.more += leafCount(e.ErrorKind)
		return
	}
	path := ""
	for _, msg := range leafMessages(e.ErrorKind) {
		if len(r.out) >= maxReportedErrors {
			r.more++
			continue
		}
		if path == "" {
			path = instancePath(r.root, e.InstanceLocation, r.instance, r.names)
		}
		line := path + ": " + msg
		// anyOf and allOf branches often fail the same way.
		if r.seen[line] {
			continue
		}
		r.seen[line] = true
		r.out = append(r.out, line)
	}
}

// opaqueKind reports kinds rendered as one entry even though they carry
// causes: their causes describe items that failed contains, or the property
// name string checked by propertyNames, and read as errors of their own.
func opaqueKind(k jsonschema.ErrorKind) bool {
	switch k.(type) {
	case *kind.Contains, *kind.MinContains, *kind.PropertyNames, *kind.ContentSchema:
		return true
	}
	return false
}

// leafMessages returns the messages for one leaf error: one per missing
// property for required, one otherwise.
func leafMessages(k jsonschema.ErrorKind) []string {
	if req, ok := k.(*kind.Required); ok && len(req.Missing) > 0 {
		out := make([]string, len(req.Missing))
		for i, name := range req.Missing {
			out[i] = "missing required property " + quoteJSONString(name)
		}
		return out
	}
	return []string{leafMessage(k)}
}

// leafCount is len(leafMessages(k)) without rendering them.
func leafCount(k jsonschema.ErrorKind) int {
	if req, ok := k.(*kind.Required); ok && len(req.Missing) > 0 {
		return len(req.Missing)
	}
	return 1
}

// leafMessage renders one error kind from schema-side values only.
func leafMessage(k jsonschema.ErrorKind) string {
	switch k := k.(type) {
	case *kind.Type:
		return "must be " + strings.Join(k.Want, " or ")
	case *kind.Minimum:
		return "must be >= " + ratString(k.Want)
	case *kind.Maximum:
		return "must be <= " + ratString(k.Want)
	case *kind.ExclusiveMinimum:
		return "must be > " + ratString(k.Want)
	case *kind.ExclusiveMaximum:
		return "must be < " + ratString(k.Want)
	case *kind.MultipleOf:
		return "must be a multiple of " + ratString(k.Want)
	case *kind.MinLength:
		return "must be at least " + countOf(k.Want, "character", "characters")
	case *kind.MaxLength:
		return "must be at most " + countOf(k.Want, "character", "characters")
	case *kind.MinItems:
		return "must have at least " + countOf(k.Want, "item", "items")
	case *kind.MaxItems:
		return "must have at most " + countOf(k.Want, "item", "items")
	case *kind.MinProperties:
		return "must have at least " + countOf(k.Want, "property", "properties")
	case *kind.MaxProperties:
		return "must have at most " + countOf(k.Want, "property", "properties")
	case *kind.Enum:
		return "must be one of the allowed values"
	case *kind.Const:
		return "must equal the allowed value"
	case *kind.Pattern:
		return "must match the required pattern"
	case *kind.Format:
		return "must be a valid " + k.Want
	case *kind.AdditionalProperties:
		return "has properties that are not allowed"
	case *kind.FalseSchema:
		return "is not allowed"
	case *kind.Not:
		return `does not satisfy "not"`
	}
	if path := k.KeywordPath(); len(path) > 0 {
		return "does not satisfy " + quoteJSONString(path[0])
	}
	return "does not satisfy the schema"
}

func countOf(n int, singular, plural string) string {
	if n == 1 {
		return "1 " + singular
	}
	return strconv.Itoa(n) + " " + plural
}

// maxExactRatBits bounds the numbers ratString prints digit for digit.
const maxExactRatBits = 256

// ratString prints a schema number such as a minimum. Schema numbers come
// from JSON decimals, so they print exactly unless they are huge, which
// print in exponent form.
func ratString(r *big.Rat) string {
	if r == nil {
		return "?"
	}
	if r.Num().BitLen() <= maxExactRatBits && r.Denom().BitLen() <= maxExactRatBits {
		if r.IsInt() {
			return r.Num().String()
		}
		if prec, exact := r.FloatPrec(); exact {
			return r.FloatString(prec)
		}
	}
	// A zero precision would make SetRat use the operands' full bit length.
	return new(big.Float).SetPrec(64).SetRat(r).Text('g', 17)
}

// instancePath renders an instance location in dot form from root. instance
// is the validated value, walked alongside so array indexes render as [i].
// Object keys render only when names is nil or holds them, else as "*".
func instancePath(root string, tokens []string, instance any, names map[string]struct{}) string {
	var b strings.Builder
	b.WriteString(root)
	cur := instance
	for _, tok := range tokens {
		switch node := cur.(type) {
		case []any:
			i, err := strconv.Atoi(tok)
			if err != nil || i < 0 || i >= len(node) {
				b.WriteString("[*]")
				cur = nil
				continue
			}
			b.WriteString("[" + tok + "]")
			cur = node[i]
		case map[string]any:
			if _, named := names[tok]; names == nil || named {
				writePathKey(&b, tok)
			} else {
				b.WriteString(".*")
			}
			cur = node[tok]
		default:
			// Locations always follow the instance; never echo a token that
			// can't be checked against it.
			b.WriteString(".*")
			cur = nil
		}
	}
	return b.String()
}

// appendPathKey appends an object key to a dot path.
func appendPathKey(path, key string) string {
	var b strings.Builder
	b.WriteString(path)
	writePathKey(&b, key)
	return b.String()
}

// indexPath appends an array index to a dot path.
func indexPath(path string, i int) string {
	return path + "[" + strconv.Itoa(i) + "]"
}

// writePathKey writes .key for keys made of letters, digits, _, $ and -, and
// ["key"] for anything else, so paths stay unambiguous.
func writePathKey(b *strings.Builder, key string) {
	if plainPathKey(key) {
		b.WriteByte('.')
		b.WriteString(key)
		return
	}
	b.WriteByte('[')
	b.WriteString(quoteJSONString(key))
	b.WriteByte(']')
}

func plainPathKey(key string) bool {
	if key == "" {
		return false
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '_', c == '$', c == '-':
		default:
			return false
		}
	}
	return true
}

// quoteJSONString quotes s as a JSON string without HTML escaping.
func quoteJSONString(s string) string {
	b, err := marshalNoEscape(s)
	if err != nil {
		return strconv.Quote(s)
	}
	return string(b)
}
