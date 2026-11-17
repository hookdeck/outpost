package safejmespath

import (
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/jmespath/go-jmespath"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func preorderKinds(n astNode) []int {
	kinds := []int{n.kind()}
	for _, c := range n.children() {
		kinds = append(kinds, preorderKinds(c)...)
	}
	return kinds
}

// The node type numbers are copied from go-jmespath, which does not export
// them. The library names every node in its pretty printer, in pre-order, so
// the numbers are checked against those names.
func TestNodeKindsMatchLibrary(t *testing.T) {
	t.Parallel()

	byName := map[string]int{
		"ASTEmpty":              kindEmpty,
		"ASTComparator":         kindComparator,
		"ASTCurrentNode":        kindCurrentNode,
		"ASTExpRef":             kindExpRef,
		"ASTFunctionExpression": kindFunctionExpression,
		"ASTField":              kindField,
		"ASTFilterProjection":   kindFilterProjection,
		"ASTFlatten":            kindFlatten,
		"ASTIdentity":           kindIdentity,
		"ASTIndex":              kindIndex,
		"ASTIndexExpression":    kindIndexExpression,
		"ASTKeyValPair":         kindKeyValPair,
		"ASTLiteral":            kindLiteral,
		"ASTMultiSelectHash":    kindMultiSelectHash,
		"ASTMultiSelectList":    kindMultiSelectList,
		"ASTOrExpression":       kindOrExpression,
		"ASTAndExpression":      kindAndExpression,
		"ASTNotExpression":      kindNotExpression,
		"ASTPipe":               kindPipe,
		"ASTProjection":         kindProjection,
		"ASTSubexpression":      kindSubexpression,
		"ASTSlice":              kindSlice,
		"ASTValueProjection":    kindValueProjection,
	}

	expressions := []string{
		"a == b",
		"@",
		"length(a)",
		"map(&a, b)",
		"a.b",
		"a[?b]",
		"a[]",
		"*.a",
		"a[0]",
		"a[1:2]",
		"`1`",
		"'x'",
		"{k: a}",
		"[a, b]",
		"a || b",
		"a && b",
		"!a",
		"a | b",
		"a[*].b",
		"a.*",
		"[0]",
	}

	namePattern := regexp.MustCompile(`(?m)^\s*(AST\w+) \{$`)
	seen := map[string]bool{}
	for _, expression := range expressions {
		root, err := jmespath.NewParser().Parse(expression)
		require.NoError(t, err, expression)

		var names []string
		for _, m := range namePattern.FindAllStringSubmatch(root.String(), -1) {
			names = append(names, m[1])
		}
		kinds := preorderKinds(astNode{reflect.ValueOf(root)})
		require.Equal(t, len(names), len(kinds), expression)
		for i, name := range names {
			want, ok := byName[name]
			require.True(t, ok, "unknown node type %s", name)
			assert.Equal(t, want, kinds[i], "%s in %q", name, expression)
			seen[name] = true
		}
	}
	// The parser never produces an empty node; the zero value is one.
	assert.Equal(t, kindEmpty, astNode{reflect.ValueOf(jmespath.ASTNode{})}.kind())
	seen["ASTEmpty"] = true
	for name := range byName {
		assert.True(t, seen[name], "no expression covers %s", name)
	}
}

func TestCheck_Accepts(t *testing.T) {
	t.Parallel()

	refs := func(n int) string {
		return "join('', [" + strings.TrimSuffix(strings.Repeat("data.a, ", n), ", ") + "])"
	}
	templates := []string{
		// The default key template and the templates in the docs and tests.
		`join('', [time.rfc3339_nano, '_', metadata."event-id", '.json'])`,
		`join('', ['events/', metadata."event-id", '.json'])`,
		`join('/', [data.customer_id, metadata."event-id"])`,
		`data.customer_id`,
		`join('__', [metadata.topic, metadata.timestamp, metadata."event-id"])`,
		`join('/', ['year=', time.year, 'month=', time.month, 'day=', time.day, metadata."event-id", '.json'])`,
		`join('/', [metadata.topic, time.date, metadata."event-id"])`,
		`join('/', ['users', data.user_id, 'actions', data.action, metadata."event-id"])`,
		`join('/', [metadata.env, metadata.region, time.year, time.month, metadata.topic, data.customer_id, join('_', [data.order_id, time.unix])])`,
		`join('/', ['prefix', metadata.operationId])`,
		`join('', ['logs/', time.year, '/', time.month, '/', metadata.topic, '/', time.day, '_', metadata."event-id", '.gz'])`,
		`join('', ['static-key-', time.date, '.json'])`,
		`join('/', ['data', 'raw', metadata.topic, time.date, metadata."event-id"])`,
		`join('/', ['test', time.rfc3339_nano, metadata."event-id", '.json'])`,
		`metadata."event-id"`,
		`time.rfc3339_nano`,
		`data.nonexistent`,
		`data.empty`,
		`data.count`,
		`data.active`,
		// The grammar.
		`data`,
		`data.user.id`,
		`data.items[0].sku`,
		`data.items[-1]`,
		`data.matrix[0][1]`,
		`data."a b".c`,
		`not_null(data.tenant, 'default')`,
		`not_null(data.a, data.b, metadata."event-id")`,
		`data.tenant || 'default'`,
		`data.a || data.b || 'x'`,
		`to_string(data.count)`,
		`to_string(data)`,
		`join('/', ['a', join('-', [data.a, join('.', [data.b, 'c'])]), 'z'])`,
		`join('', ['prefix-', data.a, '-suffix'])`,
		`join(` + "`\"/\"`" + `, [data.a, data.b])`,
		`join('/', [to_string(data.n), not_null(data.a, 'x'), data.b || 'y'])`,
		// A path inside to_string counts six times: five fit in the limit of 32.
		`join('', [to_string(data), to_string(data), to_string(data), to_string(data), to_string(data), data.a, data.b])`,
		"`1`",
		"`true`",
		"`null`",
		"`\"text\"`",
		`'text'`,
		refs(MaxReferences),
	}
	for _, template := range templates {
		assert.NoError(t, Check(template), template)
		compiled, err := Compile(template)
		require.NoError(t, err, template)
		assert.NotNil(t, compiled)
	}
}

func TestCheck_Rejects(t *testing.T) {
	t.Parallel()

	templates := []string{
		// Current node, pipes and projections.
		`@`,
		`data | @`,
		`data.a | data.b`,
		`data[*]`,
		`data[*].a`,
		`data.*`,
		`data[]`,
		`data[0][]`,
		`data[?a == 'b']`,
		`data[?a]`,
		`data[1:2]`,
		`data[::2]`,
		`data.a[0:1].b`,
		`*`,
		`[0]`,
		`data.a.[b, c]`,
		// Multi-select outside join.
		`[data.a, data.b]`,
		`{a: data.a}`,
		`data.{a: b}`,
		`to_string([data.a])`,
		`not_null([data.a])`,
		`data.a || [data.b]`,
		// join shapes.
		`join(data.sep, [data.a])`,
		`join(metadata."event-id", [data.a])`,
		"join(`1`, [data.a])",
		"join(`null`, [data.a])",
		`join('/', data.items)`,
		`join('/', data.a || [data.b])`,
		`join('/', [data.a], 'x')`,
		`join('/')`,
		`join('/', [data.items[*]])`,
		`join('/', [[data.a]])`,
		`join('/', [data.a | data.b])`,
		`join('/', [map(&a, data.b)])`,
		`join('/', {a: data.a})`,
		// Functions.
		`map(&a, data.items)`,
		`sort_by(data.items, &a)`,
		`max_by(data.items, &a)`,
		`min_by(data.items, &a)`,
		`merge(data, data)`,
		`length(data)`,
		`contains(data.a, 'x')`,
		`keys(data)`,
		`values(data)`,
		`sort(data.items)`,
		`reverse(data.items)`,
		`to_array(data)`,
		`to_number(data.a)`,
		`sum(data.items)`,
		`avg(data.items)`,
		`min(data.items)`,
		`max(data.items)`,
		`abs(data.a)`,
		`ceil(data.a)`,
		`floor(data.a)`,
		`type(data)`,
		`starts_with(data.a, 'x')`,
		`ends_with(data.a, 'x')`,
		`frobnicate(data)`,
		`data.a.to_string(data.b)`,
		"`1`(data)",
		// Arity.
		// Six paths inside to_string exceed the limit of 32.
		`join('', [to_string(data), to_string(data), to_string(data), to_string(data), to_string(data), to_string(data)])`,
		`to_string()`,
		`to_string(data.a, data.b)`,
		`not_null()`,
		// Expression references, operators.
		`&data.a`,
		`to_string(&data.a)`,
		`data.a == data.b`,
		`data.a != 'x'`,
		`data.a < 1`,
		`data.a && data.b`,
		`!data.a`,
		`data.a || (data.b && data.c)`,
		// Literals.
		"`[1, 2]`",
		"`{\"a\": 1}`",
		"`[]`",
		"to_string(`[1]`)",
		"join('/', [`{}`])",
		// Paths that are not plain.
		`'x'[0]`,
		"`1`.a",
		`to_string(data).a`,
		`to_string(data)[0]`,
		`join('/', [data.a]).b`,
		`(data.a || data.b).c`,
		`(data.a || data.b)[0]`,
		// Not parseable.
		``,
		`data.`,
		`join('/', [data.a`,
		`data.[`,
		`invalid[template`,
	}
	for _, template := range templates {
		assert.Error(t, Check(template), template)
		_, err := Compile(template)
		assert.Error(t, err, template)
	}

	// Too many references, and the error that tells a parsable template from
	// one that is not.
	tooMany := "join('', [" + strings.TrimSuffix(strings.Repeat("data.a, ", MaxReferences+1), ", ") + "])"
	assert.ErrorIs(t, Check(tooMany), ErrNotAccepted)
	assert.ErrorIs(t, Check("join('/', [data.a, data.items[*]])"), ErrNotAccepted)
	assert.NotErrorIs(t, Check("invalid[template"), ErrNotAccepted)
	assert.NotErrorIs(t, Check("data."), ErrNotAccepted)
}

func TestCheck_DeepTemplateDoesNotPanic(t *testing.T) {
	t.Parallel()

	deep := strings.Repeat("(", 500) + "data" + strings.Repeat(")", 500)
	assert.NoError(t, Check(deep))
	assert.Error(t, Check(strings.Repeat("to_string(", 500)+"data.a, data.b"+strings.Repeat(")", 500)))
}
