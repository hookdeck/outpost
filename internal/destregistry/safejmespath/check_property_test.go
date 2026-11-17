package safejmespath

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/jmespath/go-jmespath"
	"github.com/stretchr/testify/require"
)

// g is a template under construction. The generators below build g values,
// render them to JMESPath, and the recognizer decides from the g itself (not
// from the parser) whether the template is in the accepted forms. Check must
// agree with it. Every composite renders inside parentheses, which the parser
// drops, so the parsed tree has the shape of the g.
type g struct {
	op   string
	s    string
	n    int // json: 1 scalar, 2 string, 0 other
	kids []*g
}

func (x *g) render() string {
	args := func() string {
		parts := make([]string, len(x.kids))
		for i, k := range x.kids {
			parts[i] = k.render()
		}
		return strings.Join(parts, ", ")
	}
	one := func(i int) string { return x.kids[i].render() }
	switch x.op {
	case "field":
		return quoteField(x.s)
	case "path", "current":
		return x.s
	case "raw":
		return "'" + x.s + "'"
	case "json":
		return "`" + x.s + "`"
	case "sub":
		if x.kids[1].op == "field" {
			return "(" + one(0) + ")." + x.kids[1].render()
		}
		return "(" + one(0) + ")." + one(1)
	case "index":
		return "(" + one(0) + ")[" + strconv.Itoa(x.n) + "]"
	case "slice":
		return "(" + one(0) + ")[1:2]"
	case "wild":
		return "(" + one(0) + ")[*]"
	case "valwild":
		return "(" + one(0) + ").*"
	case "flatten":
		return "(" + one(0) + ")[]"
	case "filter":
		return "(" + one(0) + ")[?" + one(1) + "]"
	case "fn":
		return "(" + x.s + "(" + args() + "))"
	case "or":
		return "(" + one(0) + " || " + one(1) + ")"
	case "and":
		return "(" + one(0) + " && " + one(1) + ")"
	case "pipe":
		return "(" + one(0) + " | " + one(1) + ")"
	case "cmp":
		return "(" + one(0) + " " + x.s + " " + one(1) + ")"
	case "not":
		return "(!" + one(0) + ")"
	case "list":
		return "[" + args() + "]"
	case "hash":
		return "{k: " + one(0) + "}"
	case "ref":
		return "&" + one(0)
	}
	panic("unknown op " + x.op)
}

func quoteField(name string) string {
	if strings.ContainsAny(name, "- ") {
		return `"` + name + `"`
	}
	return name
}

// accepted is the independent statement of the accepted forms, over g.
type accepted struct{ refs int }

func (r *accepted) scalar(x *g) bool {
	switch x.op {
	case "field", "sub", "index", "path":
		r.refs++
		return isPath(x)
	case "raw":
		return true
	case "json":
		return x.n != 0
	case "or":
		return r.scalar(x.kids[0]) && r.scalar(x.kids[1])
	case "fn":
		switch x.s {
		case "to_string":
			before := r.refs
			ok := len(x.kids) == 1 && r.scalar(x.kids[0])
			r.refs += (toStringWeight - 1) * (r.refs - before)
			return ok
		case "not_null":
			return len(x.kids) >= 1 && r.all(x.kids)
		case "join":
			if len(x.kids) != 2 {
				return false
			}
			sep := x.kids[0]
			if !(sep.op == "raw" || (sep.op == "json" && sep.n == 2)) {
				return false
			}
			return x.kids[1].op == "list" && len(x.kids[1].kids) >= 1 && r.all(x.kids[1].kids)
		}
	}
	return false
}

func (r *accepted) all(xs []*g) bool {
	ok := true
	for _, x := range xs {
		if !r.scalar(x) {
			ok = false // keep counting: a template over the limit is rejected either way
		}
	}
	return ok
}

func isPath(x *g) bool {
	switch x.op {
	case "field", "path":
		return true
	case "sub":
		return x.kids[1].op == "field" && isPath(x.kids[0])
	case "index":
		return isPath(x.kids[0])
	}
	return false
}

// literal bytes a template can add to its result (a join separator once per item).
func literalBytes(x *g) int {
	total := 0
	switch x.op {
	case "raw":
		total = len(x.s)
	case "json":
		total = len(x.s)
	case "fn":
		if x.s == "join" && len(x.kids) == 2 && x.kids[1].op == "list" {
			total += len(x.kids[0].s) * len(x.kids[1].kids)
		}
	}
	for _, k := range x.kids {
		total += literalBytes(k)
	}
	return total
}

var (
	fieldNames = []string{"a", "b", "items", "user", "id", "k0", "k1", "event-id", "missing", "a b", "matrix"}
	roots      = []string{"data", "data", "data", "metadata", "time"}
	allFuncs   = []string{
		"to_string", "to_string", "not_null", "not_null", "join", "join", "map", "sort_by", "max_by", "min_by", "merge",
		"length", "contains", "keys", "values", "sort", "reverse", "to_array", "to_number", "sum", "avg", "min", "max",
		"abs", "ceil", "floor", "type", "starts_with", "ends_with", "frobnicate",
	}
	literals = []struct {
		text string
		n    int
	}{
		{`"/"`, 2}, {`"x"`, 2}, {`1`, 1}, {`true`, 1}, {`null`, 1}, {`[1]`, 0}, {`{"a":1}`, 0}, {`[]`, 0},
	}
)

func pick(r *rand.Rand, xs []string) string { return xs[r.Intn(len(xs))] }

// genPath renders a natural path such as data.items[0].a.
func genPath(r *rand.Rand) *g {
	var b strings.Builder
	b.WriteString(pick(r, roots))
	for i, steps := 0, r.Intn(4); i < steps; i++ {
		if r.Intn(4) == 0 {
			fmt.Fprintf(&b, "[%d]", r.Intn(5)-2)
		} else {
			b.WriteString("." + quoteField(pick(r, fieldNames)))
		}
	}
	return &g{op: "path", s: b.String()}
}

func genRaw(r *rand.Rand) *g {
	return &g{op: "raw", s: pick(r, []string{"/", "-", "_", "prefix", "a b", "", ".json", "x<y>&z"})}
}

// genAccepted builds a template of the accepted forms.
func genAccepted(r *rand.Rand, depth int) *g {
	n := r.Intn(10)
	if depth <= 0 {
		n = r.Intn(3)
	}
	switch n {
	case 0, 1:
		return genPath(r)
	case 2:
		if r.Intn(2) == 0 {
			return genRaw(r)
		}
		lit := literals[r.Intn(5)]
		return &g{op: "json", s: lit.text, n: lit.n}
	case 3:
		return &g{op: "fn", s: "to_string", kids: []*g{genAccepted(r, depth-1)}}
	case 4:
		kids := make([]*g, 1+r.Intn(3))
		for i := range kids {
			kids[i] = genAccepted(r, depth-1)
		}
		return &g{op: "fn", s: "not_null", kids: kids}
	case 5:
		return &g{op: "or", kids: []*g{genAccepted(r, depth-1), genAccepted(r, depth-1)}}
	default:
		items := make([]*g, 1+r.Intn(6))
		for i := range items {
			items[i] = genAccepted(r, depth-1)
		}
		sep := genRaw(r)
		if r.Intn(4) == 0 {
			sep = &g{op: "json", s: `"/"`, n: 2}
		}
		return &g{op: "fn", s: "join", kids: []*g{sep, {op: "list", kids: items}}}
	}
}

// genFull builds an expression from the whole JMESPath grammar, mixing in
// accepted subtrees so that some results are accepted.
func genFull(r *rand.Rand, depth int) *g {
	if depth <= 0 || r.Intn(6) == 0 {
		switch r.Intn(6) {
		case 0:
			return &g{op: "current", s: "@"}
		case 1:
			return &g{op: "field", s: pick(r, fieldNames)}
		case 2:
			return genRaw(r)
		case 3:
			lit := literals[r.Intn(len(literals))]
			return &g{op: "json", s: lit.text, n: lit.n}
		}
		return genPath(r)
	}
	sub := func() *g { return genFull(r, depth-1) }
	switch r.Intn(18) {
	case 0:
		return &g{op: "sub", kids: []*g{sub(), {op: "field", s: pick(r, fieldNames)}}}
	case 1:
		return &g{op: "sub", kids: []*g{sub(), {op: "list", kids: []*g{sub(), sub()}}}}
	case 2:
		return &g{op: "index", n: r.Intn(5) - 2, kids: []*g{sub()}}
	case 3:
		return &g{op: []string{"slice", "wild", "valwild", "flatten"}[r.Intn(4)], kids: []*g{sub()}}
	case 4:
		return &g{op: "filter", kids: []*g{sub(), sub()}}
	case 5:
		return &g{op: "or", kids: []*g{sub(), sub()}}
	case 6:
		return &g{op: pick2(r, "and", "pipe"), kids: []*g{sub(), sub()}}
	case 7:
		return &g{op: "cmp", s: pick(r, []string{"==", "!=", "<", ">="}), kids: []*g{sub(), sub()}}
	case 8:
		return &g{op: "not", kids: []*g{sub()}}
	case 9:
		return &g{op: "list", kids: []*g{sub(), sub()}}
	case 10:
		return &g{op: "hash", kids: []*g{sub()}}
	case 11:
		return &g{op: "fn", s: "map", kids: []*g{{op: "ref", kids: []*g{sub()}}, sub()}}
	case 12, 13:
		return genAccepted(r, depth)
	default:
		name := pick(r, allFuncs)
		kids := make([]*g, r.Intn(4))
		for i := range kids {
			kids[i] = sub()
		}
		if name == "join" && r.Intn(2) == 0 {
			kids = []*g{genRaw(r), {op: "list", kids: []*g{sub(), sub()}}}
		}
		return &g{op: "fn", s: name, kids: kids}
	}
}

func pick2(r *rand.Rand, a, b string) string {
	if r.Intn(2) == 0 {
		return a
	}
	return b
}

// genOverLimit repeats a path more than MaxReferences times in a join.
func genOverLimit(r *rand.Rand) *g {
	items := make([]*g, MaxReferences-2+r.Intn(5))
	for i := range items {
		items[i] = genPath(r)
	}
	return &g{op: "fn", s: "join", kids: []*g{genRaw(r), {op: "list", kids: items}}}
}

// Check never accepts a template that the independent statement of the
// accepted forms rejects, and accepts every one it allows.
func TestCheck_AgreesWithAcceptedForms(t *testing.T) {
	t.Parallel()

	r := rand.New(rand.NewSource(465))
	total, accepts, unparsed := 0, 0, 0
	for i := 0; i < 30000; i++ {
		var x *g
		switch {
		case i%10 == 0:
			x = genOverLimit(r)
		case i%2 == 0:
			x = genAccepted(r, 4)
		default:
			x = genFull(r, 4)
		}
		expression := x.render()
		rec := &accepted{}
		want := rec.scalar(x) && rec.refs <= MaxReferences

		err := Check(expression)
		total++
		if err == nil {
			accepts++
		} else if !errors.Is(err, ErrNotAccepted) {
			unparsed++
		}
		require.Equal(t, want, err == nil, "%s (check error: %v)", expression, err)
	}
	t.Logf("generated %d, accepted %d, not parsable %d", total, accepts, unparsed)
	require.Greater(t, accepts, total/5)
}

func adversarialInput(shape string, size int) map[string]interface{} {
	data := map[string]interface{}{
		"a": "x", "b": "y", "items": []interface{}{"i0", "i1", "i2"}, "user": map[string]interface{}{"id": "u"},
		"matrix": []interface{}{[]interface{}{"m", "n"}},
	}
	switch shape {
	case "dominant":
		data["a"] = strings.Repeat("x", size)
	case "keys":
		for i := 0; i < size/16; i++ {
			data["k"+strconv.Itoa(i)] = "v" + strconv.Itoa(i)
		}
	case "nested":
		items := make([]interface{}, size/8)
		for i := range items {
			items[i] = []interface{}{"s", []interface{}{"t", nil}}
		}
		data["items"] = items
		data["matrix"] = []interface{}{items}
	case "control":
		data["a"] = strings.Repeat("\x01<>&\"\\\n ", size/8)
		data["user"] = map[string]interface{}{"id": strings.Repeat("\x00", size/4)}
	case "nulls":
		data = map[string]interface{}{"a": nil, "b": nil, "items": make([]interface{}, size/4), "user": nil}
	}
	return map[string]interface{}{
		"data":     data,
		"metadata": map[string]interface{}{"event-id": "evt_1", "topic": "t", "k0": "m"},
		"time":     map[string]interface{}{"year": "2026", "month": "11"},
	}
}

var shapes = []string{"dominant", "keys", "nested", "control", "nulls"}

func jsonLen(t *testing.T, v interface{}) int {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return len(b)
}

// Accepted templates never grow a value past a fixed number of copies of the
// input, never panic, and allocate in proportion to the input.
func TestCheck_AcceptedTemplatesAreBounded(t *testing.T) {
	r := rand.New(rand.NewSource(4650))

	evaluated, errored, checkedAlloc := 0, 0, 0
	worstSize, worstAlloc := 0.0, 0.0
	for i := 0; i < 1500; i++ {
		x := genAccepted(r, 4)
		expression := x.render()
		rec := &accepted{}
		require.True(t, rec.scalar(x))
		if rec.refs > MaxReferences {
			continue
		}
		compiled, err := Compile(expression)
		require.NoError(t, err, expression)
		lits := literalBytes(x)

		for _, shape := range shapes {
			input := adversarialInput(shape, 3000)
			var result interface{}
			var err error
			require.NotPanics(t, func() { result, err = compiled.Search(input) }, expression)
			if err != nil {
				errored++
				continue
			}
			evaluated++
			// Each reference is at most the input; to_string re-escapes quotes
			// and backslashes, at most doubling a byte of JSON.
			limit := 2*(rec.refs*jsonLen(t, input)+lits) + 64
			size := jsonLen(t, result)
			require.LessOrEqual(t, size, limit, "%s on %s", expression, shape)
			worstSize = max(worstSize, float64(size)/float64(limit))
		}

		if i%25 != 0 {
			continue
		}
		for _, shape := range shapes {
			small, big := adversarialInput(shape, 2<<10), adversarialInput(shape, 8<<10)
			if _, err := compiled.Search(big); err != nil {
				continue
			}
			a, b := allocated(compiled, small), allocated(compiled, big)
			checkedAlloc++
			require.LessOrEqual(t, b, 4*a+32<<10, "%s on %s: %d bytes at 2 KB, %d at 8 KB", expression, shape, a, b)
			worstAlloc = max(worstAlloc, float64(b)/float64(a+1))
		}
	}
	t.Logf("evaluated %d (%d ended in an ordinary error), allocation compared %d times; worst size/limit %.2f, worst 8KB/2KB allocation ratio %.2f",
		evaluated, errored, checkedAlloc, worstSize, worstAlloc)
	require.Greater(t, evaluated, 3000)
}

// allocated is the fewest bytes allocated by an evaluation over a few runs.
func allocated(c *jmespath.JMESPath, input interface{}) uint64 {
	best := ^uint64(0)
	for i := 0; i < 3; i++ {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		_, _ = c.Search(input)
		runtime.ReadMemStats(&after)
		best = min(best, after.TotalAlloc-before.TotalAlloc)
	}
	return best
}
