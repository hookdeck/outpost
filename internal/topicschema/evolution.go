package topicschema

import (
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"maps"
	"math/big"
	"net/url"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	// maxDiffComparisons bounds the work spent diffing one topic: schema
	// pairs compared, $refs followed and subschemas visited. Past it the topic
	// gets a single "too complex" change, so a crafted $ref graph can't stall
	// startup.
	maxDiffComparisons = 100_000
	// maxChangesPerTopic bounds the changes listed for one topic.
	maxChangesPerTopic = 100
	// maxDetailValue bounds how much of a schema value a change detail quotes.
	maxDetailValue = 64
)

// BreakingChanges lists the changes from prev to next that can break
// existing MCP subscriptions to topics. Subscribers keep the payload contract
// and the filter arguments they subscribed with, so next must accept every
// payload prev accepted and keep every filterable top-level property
// filterable. Widenings, relaxed constraints, new properties (required or
// not), required changes and annotations all pass.
//
// Only topics with a payload schema in both snapshots are compared; topics
// that leave MCP or disappear are EndedTopics' concern. Local $refs are
// followed on both sides, allOf branches are merged into one schema (union of
// properties, tightest constraints), and other composite keywords (anyOf,
// oneOf, not, if/then/else, patternProperties, ...) must stay equal, the
// schemas they $ref included. Change paths are JSON pointers into prev's
// schema, where the broken contract was declared. The result is sorted by
// topic, path, kind and detail, and is nil when nothing breaks.
func BreakingChanges(prev, next Snapshot, topics []string) []Change {
	names := slices.Clone(topics)
	slices.Sort(names)
	names = slices.Compact(names)
	var out []Change
	for _, name := range names {
		p, ok := prev.Topics[name]
		if !ok || !hasSchemaJSON(p.PayloadSchema) {
			continue
		}
		n, ok := next.Topics[name]
		if !ok || !hasSchemaJSON(n.PayloadSchema) {
			continue
		}
		out = append(out, diffSchemas(name, p.PayloadSchema, n.PayloadSchema, maxDiffComparisons)...)
	}
	slices.SortFunc(out, compareChanges)
	return slices.Compact(out)
}

// EndedTopics returns the topics that are MCP-enabled in prev but absent or
// not MCP-enabled in next, sorted. Their subscriptions can't continue. A
// topic only counts as MCP-enabled with a payload schema, as the catalog
// requires.
func EndedTopics(prev, next Snapshot) []string {
	var out []string
	for name, p := range prev.Topics {
		if !mcpServed(p) {
			continue
		}
		if n, ok := next.Topics[name]; ok && mcpServed(n) {
			continue
		}
		out = append(out, name)
	}
	slices.Sort(out)
	return out
}

func mcpServed(t SnapshotTopic) bool {
	return t.MCPEnabled && hasSchemaJSON(t.PayloadSchema)
}

// Hash returns the hex SHA-256 of the snapshot's canonical JSON: topics and
// object keys sorted, payload schemas without insignificant whitespace.
// Reordering keys leaves it unchanged; any other edit changes it.
func (s Snapshot) Hash() string {
	var buf bytes.Buffer
	buf.WriteString(`{"topics":{`)
	for i, name := range slices.Sorted(maps.Keys(s.Topics)) {
		if i > 0 {
			buf.WriteByte(',')
		}
		key, _ := marshalNoEscape(name)
		buf.Write(key)
		buf.WriteByte(':')
		writeSnapshotTopic(&buf, s.Topics[name])
	}
	buf.WriteString("}}")
	return sha256Hex(buf.Bytes())
}

// TopicHash returns the hex SHA-256 of one topic's canonical
// {"mcp_enabled","payload_schema"}, or "" when the snapshot lacks the topic.
// MCP subscriptions record it to tell which schema they were created against.
func (s Snapshot) TopicHash(name string) string {
	t, ok := s.Topics[name]
	if !ok {
		return ""
	}
	var buf bytes.Buffer
	writeSnapshotTopic(&buf, t)
	return sha256Hex(buf.Bytes())
}

func writeSnapshotTopic(buf *bytes.Buffer, t SnapshotTopic) {
	buf.WriteString(`{"mcp_enabled":`)
	buf.WriteString(strconv.FormatBool(t.MCPEnabled))
	if hasSchemaJSON(t.PayloadSchema) {
		if c, err := canonicalJSON(t.PayloadSchema); err == nil {
			buf.WriteString(`,"payload_schema":`)
			buf.Write(c)
		} else {
			// Not JSON, which a catalog never produces. Hash the bytes under
			// a key of their own so they can't collide with a valid schema.
			buf.WriteString(`,"payload_schema_invalid":"`)
			buf.WriteString(hex.EncodeToString(t.PayloadSchema))
			buf.WriteByte('"')
		}
	}
	buf.WriteByte('}')
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// hasSchemaJSON reports whether raw holds a schema rather than nothing or null.
func hasSchemaJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && !bytes.Equal(trimmed, []byte("null"))
}

func compareChanges(a, b Change) int {
	return cmp.Or(
		strings.Compare(a.Topic, b.Topic),
		strings.Compare(a.Path, b.Path),
		strings.Compare(a.Kind, b.Kind),
		strings.Compare(a.Detail, b.Detail),
	)
}

// diffSchemas returns the breaking changes from prev to next, spending at
// most budget work units.
func diffSchemas(topic string, prev, next json.RawMessage, budget int) []Change {
	if bytes.Equal(prev, next) {
		return nil
	}
	prevRoot, err := decodeJSON(prev)
	if err != nil {
		return []Change{{Topic: topic, Kind: ChangeCompositeChanged, Detail: "previous schema is not valid JSON"}}
	}
	nextRoot, err := decodeJSON(next)
	if err != nil {
		return []Change{{Topic: topic, Kind: ChangeCompositeChanged, Detail: "schema is not valid JSON"}}
	}
	d := &differ{
		topic:     topic,
		roots:     [2]any{prevRoot, nextRoot},
		budget:    budget,
		visited:   map[string]struct{}{},
		resolved:  map[refKey]refResolution{},
		canonical: map[canonKey]canonForm{},
		refChecks: map[string]refCheck{},
		reported:  map[Change]struct{}{},
	}
	d.run()
	out := d.changes
	switch {
	case d.tooComplex:
		out = append(out, Change{Topic: topic, Kind: ChangeCompositeChanged, Detail: "schema too complex to compare"})
	case d.truncated:
		out = append(out, Change{Topic: topic, Kind: ChangeCompositeChanged,
			Detail: "more breaking changes not listed (limit " + strconv.Itoa(maxChangesPerTopic) + ")"})
	}
	return out
}

// docSide indexes the previous and next schema documents.
type docSide int

const (
	sidePrev docSide = iota
	sideNext
)

// schemaPointer is a JSON pointer stored as a linked list of unescaped
// tokens, so descending into a subschema is O(1) and strings are only built
// for reported changes. nil is the document root.
type schemaPointer struct {
	parent *schemaPointer
	token  string
}

func (p *schemaPointer) child(tokens ...string) *schemaPointer {
	for _, t := range tokens {
		p = &schemaPointer{parent: p, token: t}
	}
	return p
}

func (p *schemaPointer) String() string {
	var tokens []string
	for ; p != nil; p = p.parent {
		tokens = append(tokens, p.token)
	}
	var b strings.Builder
	for i := len(tokens) - 1; i >= 0; i-- {
		b.WriteByte('/')
		b.WriteString(escapePointerToken(tokens[i]))
	}
	return b.String()
}

// refPointer returns the location a local $ref that resolveLocalRef
// accepted points to.
func refPointer(ref string) *schemaPointer {
	fragment, _ := url.PathUnescape(ref[1:])
	if fragment == "" {
		return nil
	}
	var p *schemaPointer
	for _, token := range strings.Split(fragment[1:], "/") {
		p = p.child(strings.ReplaceAll(strings.ReplaceAll(token, "~1", "/"), "~0", "~"))
	}
	return p
}

// schemaEntry is one schema of a conjunction, with its location.
type schemaEntry struct {
	node any
	ptr  *schemaPointer
}

// schemaFragment is a schema object whose keywords apply to a view.
type schemaFragment struct {
	m   map[string]any
	ptr *schemaPointer
	// mergedAllOf reports that allOf was flattened into the view rather than
	// compared as a composite.
	mergedAllOf bool
}

// hasKeywords reports whether the fragment carries anything besides
// annotations and the references that were flattened.
func (f schemaFragment) hasKeywords() bool {
	for k := range f.m {
		if k == "$ref" || (k == "allOf" && f.mergedAllOf) || isAnnotation(k) {
			continue
		}
		return true
	}
	return false
}

// schemaView is a conjunction of schemas with local $refs and mergeable allOf
// branches flattened into the fragments that carry keywords.
type schemaView struct {
	frags []schemaFragment
	// at is where changes without a keyword location are reported: the first
	// fragment, else the location the view was reached from.
	at        *schemaPointer
	rejectAll bool   // a false schema is part of the conjunction
	badRef    string // the first $ref that doesn't resolve
}

// taskRole tells what a compared schema describes, for change details.
type taskRole byte

const (
	roleSchema taskRole = iota
	roleProperty
	roleItems
	roleAdditional
)

// diffTask is one pair of schemas to compare.
type diffTask struct {
	prev, next []schemaEntry
	at         *schemaPointer
	role       taskRole
}

type refKey struct {
	side docSide
	ref  string
}

type refResolution struct {
	entry schemaEntry
	ok    bool
}

type canonKey struct {
	side    docSide
	id      uintptr
	keyword string
}

type canonForm struct {
	text string
	refs []string
}

type refCheck struct {
	same bool
	refs []string // refs inside the target, compared transitively
}

// differ compares one topic's previous and next payload schemas. It walks
// pairs breadth first with an explicit queue, so deep schemas can't exhaust
// the stack, and memoizes visited pairs of schema objects, which both stops
// recursive schemas and keeps shared $defs from being compared once per path.
type differ struct {
	topic      string
	roots      [2]any
	budget     int
	stopped    bool
	tooComplex bool
	truncated  bool
	visited    map[string]struct{}
	resolved   map[refKey]refResolution
	canonical  map[canonKey]canonForm
	refChecks  map[string]refCheck
	changes    []Change
	reported   map[Change]struct{}
}

func (d *differ) run() {
	d.checkFilters()
	queue := []diffTask{{
		prev: []schemaEntry{{node: d.roots[sidePrev]}},
		next: []schemaEntry{{node: d.roots[sideNext]}},
	}}
	for i := 0; i < len(queue) && !d.stopped; i++ {
		t := queue[i]
		queue[i] = diffTask{}
		queue = d.compare(t, queue)
	}
}

// spend takes n work units from the budget and reports whether the diff may
// go on.
func (d *differ) spend(n int) bool {
	if d.stopped {
		return false
	}
	if d.budget < n {
		d.budget = 0
		d.stopped, d.tooComplex = true, true
		return false
	}
	d.budget -= n
	return true
}

func (d *differ) report(at *schemaPointer, kind, detail string) {
	if d.stopped {
		return
	}
	c := Change{Topic: d.topic, Path: at.String(), Kind: kind, Detail: detail}
	if _, dup := d.reported[c]; dup {
		return
	}
	if len(d.changes) == maxChangesPerTopic {
		d.stopped, d.truncated = true, true
		return
	}
	d.reported[c] = struct{}{}
	d.changes = append(d.changes, c)
}

// resolve resolves a local $ref in one document, once per reference.
func (d *differ) resolve(s docSide, ref string) (schemaEntry, bool) {
	key := refKey{side: s, ref: ref}
	if r, ok := d.resolved[key]; ok {
		return r.entry, r.ok
	}
	var r refResolution
	if node, err := resolveLocalRef(d.roots[s], ref); err == nil {
		r = refResolution{entry: schemaEntry{node: node, ptr: refPointer(ref)}, ok: true}
	}
	d.resolved[key] = r
	return r.entry, r.ok
}

// expand flattens a conjunction of schemas: $ref targets and the branches of
// mergeable allOf keywords join the view, each schema object at most once.
func (d *differ) expand(s docSide, entries []schemaEntry, at *schemaPointer) schemaView {
	v := schemaView{at: at}
	queue := slices.Clone(entries)
	var seen nodeSet
	for i := 0; i < len(queue); i++ {
		if !d.spend(1) {
			return v
		}
		e := queue[i]
		switch n := e.node.(type) {
		case bool:
			if !n {
				v.rejectAll = true
			}
		case map[string]any:
			if !seen.add(nodeID(n)) {
				continue
			}
			f := schemaFragment{m: n, ptr: e.ptr}
			if ref, ok := n["$ref"].(string); ok {
				if target, ok := d.resolve(s, ref); ok {
					queue = append(queue, target)
				} else if v.badRef == "" {
					v.badRef = ref
				}
			}
			if branches, ok := n["allOf"].([]any); ok && d.mergeableAllOf(s, branches) {
				f.mergedAllOf = true
				for j, b := range branches {
					queue = append(queue, schemaEntry{node: b, ptr: e.ptr.child("allOf", strconv.Itoa(j))})
				}
			}
			if f.hasKeywords() {
				if len(v.frags) == 0 {
					v.at = f.ptr
				}
				v.frags = append(v.frags, f)
			}
		}
	}
	return v
}

// mergeableAllOf reports whether every allOf branch is a schema whose $ref
// chain resolves, so the branches can join the view as one merged schema
// (union of properties, tightest constraints) instead of being compared by
// equality. A view is a conjunction, so merging is exact for every keyword
// the diff checks except additionalProperties, which inside allOf only sees
// its own branch's properties; merged, it errs on the lenient side.
func (d *differ) mergeableAllOf(s docSide, branches []any) bool {
	for _, b := range branches {
		node := b
		for depth := 0; ; depth++ {
			if !d.spend(1) {
				return false
			}
			if _, ok := node.(bool); ok {
				break
			}
			m, ok := node.(map[string]any)
			if !ok {
				return false
			}
			ref, ok := m["$ref"].(string)
			if !ok {
				break
			}
			target, ok := d.resolve(s, ref)
			if !ok || depth == maxRefDepth {
				return false
			}
			node = target.node
		}
	}
	return len(branches) > 0
}

// memoKey identifies the schema objects a comparison depends on. Every change
// it reports is located in prev's fragments, so pairs reached again through
// another path or a recursive $ref add nothing.
func (d *differ) memoKey(pv, nv schemaView, role taskRole) string {
	b := make([]byte, 0, 16+8*(len(pv.frags)+len(nv.frags)))
	b = append(b, byte(role))
	b = binary.AppendUvarint(b, uint64(len(pv.frags)))
	for _, f := range pv.frags {
		b = binary.LittleEndian.AppendUint64(b, uint64(nodeID(f.m)))
	}
	if nv.rejectAll {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	if nv.badRef != "" || pv.badRef != "" {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	for _, f := range nv.frags {
		b = binary.LittleEndian.AppendUint64(b, uint64(nodeID(f.m)))
	}
	return string(b)
}

// nodeID returns the identity of a decoded schema object. Both documents stay
// alive for the whole diff, so identities are unique and stable; two paths
// reaching the same object, as $refs do, get the same identity.
func nodeID(m map[string]any) uintptr {
	return uintptr(reflect.ValueOf(m).UnsafePointer())
}

// nodeSet is a set of node identities. Most sets hold a handful, so it stays
// a slice until it grows.
type nodeSet struct {
	small []uintptr
	large map[uintptr]struct{}
}

// add adds id and reports whether it was new.
func (s *nodeSet) add(id uintptr) bool {
	if s.large == nil {
		if slices.Contains(s.small, id) {
			return false
		}
		if len(s.small) < 16 {
			s.small = append(s.small, id)
			return true
		}
		s.large = make(map[uintptr]struct{}, 2*len(s.small))
		for _, x := range s.small {
			s.large[x] = struct{}{}
		}
		s.small = nil
	}
	if _, dup := s.large[id]; dup {
		return false
	}
	s.large[id] = struct{}{}
	return true
}

// compare compares one pair of schemas and queues their subschemas.
func (d *differ) compare(t diffTask, queue []diffTask) []diffTask {
	pv := d.expand(sidePrev, t.prev, t.at)
	nv := d.expand(sideNext, t.next, t.at)
	if d.stopped || pv.rejectAll {
		// Nothing validated against a false schema, so nothing can break.
		return queue
	}
	// Without prev fragments nothing is queued below, so only pairs that can
	// recurse need memoizing.
	if len(pv.frags) > 0 {
		key := d.memoKey(pv, nv, t.role)
		if _, done := d.visited[key]; done {
			return queue
		}
		d.visited[key] = struct{}{}
	}

	if pv.badRef != "" || nv.badRef != "" {
		// The catalog rejects unresolvable references, so this only happens
		// with hand-edited snapshots: fall back to equality.
		if !d.sameEntries(t.prev, t.next) {
			ref := cmp.Or(pv.badRef, nv.badRef)
			d.report(pv.at, ChangeCompositeChanged, "$ref "+displayString(ref)+" does not resolve")
		}
		return queue
	}
	if nv.rejectAll {
		kind, detail := ChangeTypeNarrowed, "schema no longer accepts any value"
		switch t.role {
		case roleProperty:
			detail = "property no longer accepts any value"
		case roleItems:
			kind, detail = ChangeConstraintTightened, "array items no longer allowed"
		case roleAdditional:
			kind, detail = ChangeConstraintTightened, "additional properties no longer allowed"
		}
		d.report(pv.at, kind, detail)
		return queue
	}

	prevAllowed, nextAllowed := d.allowed(pv), d.allowed(nv)
	d.checkType(pv, nv, prevAllowed)
	d.checkAllowed(pv, prevAllowed, nextAllowed)
	for _, k := range boundKeywords {
		d.checkBound(pv, nv, k)
	}
	d.checkUniqueItems(pv, nv)
	d.checkStringKeyword(pv, nv, "pattern")
	d.checkStringKeyword(pv, nv, "format")
	d.checkMultipleOf(pv, nv)
	d.checkComposites(pv, nv)

	queue = d.queueProperties(pv, nv, queue)
	queue = d.queueSubschema(pv, nv, "items", roleItems, queue)
	queue = d.queueSubschema(pv, nv, "additionalProperties", roleAdditional, queue)
	return queue
}

// queueProperties queues every property of prev for comparison, and reports
// the ones next no longer declares.
func (d *differ) queueProperties(pv, nv schemaView, queue []diffTask) []diffTask {
	names := map[string]struct{}{}
	for _, f := range pv.frags {
		if props, ok := f.m["properties"].(map[string]any); ok {
			for name := range props {
				names[name] = struct{}{}
			}
		}
	}
	for _, name := range slices.Sorted(maps.Keys(names)) {
		if !d.spend(1) {
			return queue
		}
		prev := propertyEntries(pv, name)
		next := propertyEntries(nv, name)
		if len(next) == 0 {
			// A property declared false was never allowed in the first place.
			if !slices.ContainsFunc(prev, func(e schemaEntry) bool { return e.node == false }) {
				d.report(prev[0].ptr, ChangePropertyRemoved, "property removed")
			}
			continue
		}
		queue = append(queue, diffTask{prev: prev, next: next, at: prev[0].ptr, role: roleProperty})
	}
	return queue
}

func propertyEntries(v schemaView, name string) []schemaEntry {
	var out []schemaEntry
	for _, f := range v.frags {
		props, _ := f.m["properties"].(map[string]any)
		if s, ok := props[name]; ok {
			out = append(out, schemaEntry{node: s, ptr: f.ptr.child("properties", name)})
		}
	}
	return out
}

// queueSubschema queues the items or additionalProperties schemas for
// comparison. Without one in next anything goes, but the properties prev
// declared inside are still reported as removed, as when it becomes {}. When
// prev has none (or only true), any value was allowed, so whatever next
// constrains is reported as one change.
func (d *differ) queueSubschema(pv, nv schemaView, kw string, role taskRole, queue []diffTask) []diffTask {
	prev := keywordEntries(pv, kw)
	next := keywordEntries(nv, kw)
	if slices.ContainsFunc(prev, func(e schemaEntry) bool { return e.node != true }) {
		return append(queue, diffTask{prev: prev, next: next, at: prev[0].ptr, role: role})
	}
	if len(next) == 0 || !d.constrains(sideNext, next) {
		return queue
	}
	rejects := slices.ContainsFunc(next, func(e schemaEntry) bool { return e.node == false })
	detail := "additional properties now constrained"
	switch {
	case role == roleItems && rejects:
		detail = "array items no longer allowed"
	case role == roleItems:
		detail = "array items now constrained"
	case rejects:
		detail = "additional properties no longer allowed"
	}
	d.report(pv.at, ChangeConstraintTightened, detail)
	return queue
}

// keywordEntries returns the schemas a view's fragments give keyword kw.
func keywordEntries(v schemaView, kw string) []schemaEntry {
	var out []schemaEntry
	for _, f := range v.frags {
		switch s := f.m[kw].(type) {
		case bool, map[string]any:
			out = append(out, schemaEntry{node: s, ptr: f.ptr.child(kw)})
		}
	}
	return out
}

// constrains reports whether a conjunction of schemas rejects any value,
// ignoring required and dependentRequired since new required properties are
// allowed.
func (d *differ) constrains(s docSide, entries []schemaEntry) bool {
	queue := make([]any, 0, len(entries))
	for _, e := range entries {
		queue = append(queue, e.node)
	}
	var seen nodeSet
	for i := 0; i < len(queue); i++ {
		if !d.spend(1) {
			return true
		}
		n, ok := queue[i].(map[string]any)
		if !ok {
			if queue[i] == false {
				return true
			}
			continue
		}
		if !seen.add(nodeID(n)) {
			continue
		}
		for k, v := range n {
			if constraintKeywords[k] || (k == "uniqueItems" && v == true) {
				return true
			}
		}
		if ref, ok := n["$ref"].(string); ok {
			target, ok := d.resolve(s, ref)
			if !ok {
				return true
			}
			queue = append(queue, target.node)
		}
		if v, ok := n["allOf"]; ok {
			branches, ok := v.([]any)
			if !ok {
				return true
			}
			queue = append(queue, branches...)
		}
		if props, ok := n["properties"].(map[string]any); ok {
			for _, name := range slices.Sorted(maps.Keys(props)) {
				queue = append(queue, props[name])
			}
		}
		for _, kw := range []string{"items", "additionalProperties"} {
			if v, ok := n[kw]; ok {
				queue = append(queue, v)
			}
		}
	}
	return false
}

// Type sets as bit masks. integer is a subset of number, so number is
// integers plus fractions and integer → number is a widening.
const (
	typeNull uint8 = 1 << iota
	typeBoolean
	typeObject
	typeArray
	typeString
	typeInteger
	typeFraction

	typeNumber   = typeInteger | typeFraction
	typeAnything = typeFraction<<1 - 1
)

var typeBits = map[string]uint8{
	"null":    typeNull,
	"boolean": typeBoolean,
	"object":  typeObject,
	"array":   typeArray,
	"string":  typeString,
	"integer": typeInteger,
	"number":  typeNumber,
}

// typeMask returns the types a view allows: the intersection of its type
// keywords, and where the first one is.
func typeMask(v schemaView) (mask uint8, at *schemaPointer, set bool) {
	mask = typeAnything
	for _, f := range v.frags {
		m, ok := parseTypeMask(f.m["type"])
		if !ok {
			continue
		}
		mask &= m
		if !set {
			at, set = f.ptr, true
		}
	}
	return mask, at, set
}

func parseTypeMask(v any) (uint8, bool) {
	switch t := v.(type) {
	case string:
		m := typeBits[t]
		return m, m != 0
	case []any:
		var m uint8
		for _, x := range t {
			s, _ := x.(string)
			if typeBits[s] == 0 {
				return 0, false
			}
			m |= typeBits[s]
		}
		return m, m != 0
	}
	return 0, false
}

func typeNames(mask uint8) string {
	var names []string
	if mask&typeArray != 0 {
		names = append(names, "array")
	}
	if mask&typeBoolean != 0 {
		names = append(names, "boolean")
	}
	switch {
	case mask&typeFraction != 0:
		names = append(names, "number")
	case mask&typeInteger != 0:
		names = append(names, "integer")
	}
	if mask&typeNull != 0 {
		names = append(names, "null")
	}
	if mask&typeObject != 0 {
		names = append(names, "object")
	}
	if mask&typeString != 0 {
		names = append(names, "string")
	}
	if len(names) == 0 {
		return "nothing"
	}
	return strings.Join(names, " or ")
}

// valueTypeMask returns the type bit of a decoded JSON value. 1.0 is an
// integer, as in JSON Schema.
func valueTypeMask(v any) uint8 {
	switch x := v.(type) {
	case nil:
		return typeNull
	case bool:
		return typeBoolean
	case string:
		return typeString
	case map[string]any:
		return typeObject
	case []any:
		return typeArray
	case json.Number:
		if n, ok := parseJSONDecimal(string(x)); ok && n.exp < int64(len(n.digits)) {
			return typeFraction
		}
		return typeInteger
	}
	return 0
}

// checkType reports types prev allowed that next doesn't. prev's types are
// narrowed by its enum or const first: {"const": "a"} only ever allowed a
// string, so adding "type": "string" takes nothing away.
func (d *differ) checkType(pv, nv schemaView, prevAllowed allowedSet) {
	prev, at, set := typeMask(pv)
	if prevAllowed.keys != nil {
		var values uint8
		for _, v := range prevAllowed.values {
			values |= valueTypeMask(v.value)
		}
		prev &= values
		if !set {
			at, set = prevAllowed.at, true
		}
	}
	next, _, _ := typeMask(nv)
	if prev&^next == 0 {
		return
	}
	if !set {
		d.report(pv.at, ChangeTypeNarrowed, "type restricted to "+typeNames(next))
		return
	}
	d.report(at, ChangeTypeNarrowed, "type changed from "+typeNames(prev)+" to "+typeNames(next))
}

// allowedValue is one value an enum or const allows, keyed by its canonical
// form so 1 and 1.0 are the same value.
type allowedValue struct {
	key   string
	value any
}

// allowedSet is what a view's enum and const keywords allow.
type allowedSet struct {
	keys     map[string]struct{} // nil when unrestricted
	values   []allowedValue      // in document order
	at       *schemaPointer
	enum     bool
	constant bool
	constVal any
}

func (d *differ) allowed(v schemaView) allowedSet {
	var a allowedSet
	for _, f := range v.frags {
		if e, ok := f.m["enum"].([]any); ok {
			a.enum = true
			d.restrict(&a, e, f.ptr)
		}
		if c, ok := f.m["const"]; ok {
			if !a.constant {
				a.constant, a.constVal = true, c
			}
			d.restrict(&a, []any{c}, f.ptr)
		}
	}
	return a
}

// restrict intersects the allowed set with values.
func (d *differ) restrict(a *allowedSet, values []any, at *schemaPointer) {
	if !d.spend(len(values)) {
		return
	}
	keys := make(map[string]struct{}, len(values))
	var list []allowedValue
	for _, v := range values {
		k := valueKey(v)
		if _, dup := keys[k]; !dup {
			keys[k] = struct{}{}
			list = append(list, allowedValue{key: k, value: v})
		}
	}
	if a.keys == nil {
		a.keys, a.values, a.at = keys, list, at
		return
	}
	a.values = slices.DeleteFunc(a.values, func(v allowedValue) bool {
		_, ok := keys[v.key]
		return !ok
	})
	a.keys = make(map[string]struct{}, len(a.values))
	for _, v := range a.values {
		a.keys[v.key] = struct{}{}
	}
}

func (d *differ) checkAllowed(pv schemaView, prev, next allowedSet) {
	if next.keys == nil {
		return
	}
	if prev.keys == nil {
		if next.constant && !next.enum {
			d.report(pv.at, ChangeConstChanged, "const "+displayValue(next.constVal)+" added")
		} else {
			d.report(pv.at, ChangeEnumNarrowed, "enum added")
		}
		return
	}
	var removed []any
	for _, v := range prev.values {
		if _, ok := next.keys[v.key]; !ok {
			removed = append(removed, v.value)
		}
	}
	if len(removed) == 0 {
		return
	}
	if next.constant && (!prev.constant || valueKey(prev.constVal) != valueKey(next.constVal)) {
		if prev.constant {
			d.report(prev.at, ChangeConstChanged,
				"const changed from "+displayValue(prev.constVal)+" to "+displayValue(next.constVal))
		} else {
			d.report(prev.at, ChangeConstChanged, "const "+displayValue(next.constVal)+" added")
		}
		return
	}
	for _, v := range removed {
		if prev.enum {
			d.report(prev.at, ChangeEnumNarrowed, "enum value "+displayValue(v)+" removed")
		} else {
			d.report(prev.at, ChangeEnumNarrowed, "value "+displayValue(v)+" no longer allowed")
		}
	}
}

// boundKeyword is a family of keywords bounding a value from one side: an
// inclusive keyword and, for numbers, an exclusive one.
type boundKeyword struct {
	inclusive, exclusive string
	lower                bool
}

var boundKeywords = []boundKeyword{
	{inclusive: "minimum", exclusive: "exclusiveMinimum", lower: true},
	{inclusive: "maximum", exclusive: "exclusiveMaximum"},
	{inclusive: "minLength", lower: true},
	{inclusive: "maxLength"},
	{inclusive: "minItems", lower: true},
	{inclusive: "maxItems"},
	{inclusive: "minProperties", lower: true},
	{inclusive: "maxProperties"},
}

type bound struct {
	value     jsonDecimal
	exclusive bool
	keyword   string
	text      string
	at        *schemaPointer
}

// tighter reports whether a admits fewer values than b.
func (a bound) tighter(b bound, lower bool) bool {
	c := a.value.cmp(b.value)
	if !lower {
		c = -c
	}
	return c > 0 || (c == 0 && a.exclusive && !b.exclusive)
}

// effectiveBound returns the tightest bound of the family across a view.
func effectiveBound(v schemaView, k boundKeyword) (bound, bool) {
	var best bound
	found := false
	for _, f := range v.frags {
		for _, kw := range []string{k.inclusive, k.exclusive} {
			if kw == "" {
				continue
			}
			n, ok := f.m[kw].(json.Number)
			if !ok {
				continue
			}
			value, ok := parseJSONDecimal(string(n))
			if !ok {
				continue
			}
			b := bound{value: value, exclusive: kw == k.exclusive, keyword: kw, text: string(n), at: f.ptr}
			if !found || b.tighter(best, k.lower) {
				best, found = b, true
			}
		}
	}
	return best, found
}

func (d *differ) checkBound(pv, nv schemaView, k boundKeyword) {
	next, ok := effectiveBound(nv, k)
	if !ok {
		return
	}
	prev, ok := effectiveBound(pv, k)
	switch {
	case !ok:
		d.report(pv.at, ChangeConstraintTightened, next.keyword+" "+truncateDetail(next.text)+" added")
	case !next.tighter(prev, k.lower):
	case prev.keyword == next.keyword:
		verb := " lowered from "
		if k.lower {
			verb = " raised from "
		}
		d.report(prev.at, ChangeConstraintTightened,
			prev.keyword+verb+truncateDetail(prev.text)+" to "+truncateDetail(next.text))
	default:
		d.report(prev.at, ChangeConstraintTightened,
			prev.keyword+" "+truncateDetail(prev.text)+" tightened to "+next.keyword+" "+truncateDetail(next.text))
	}
}

func (d *differ) checkUniqueItems(pv, nv schemaView) {
	unique := func(v schemaView) bool {
		return slices.ContainsFunc(v.frags, func(f schemaFragment) bool { return f.m["uniqueItems"] == true })
	}
	if unique(nv) && !unique(pv) {
		d.report(pv.at, ChangeConstraintTightened, "uniqueItems added")
	}
}

// checkStringKeyword reports pattern or format values next adds that prev
// didn't have.
func (d *differ) checkStringKeyword(pv, nv schemaView, kw string) {
	var prev []string
	var at *schemaPointer
	for _, f := range pv.frags {
		if s, ok := f.m[kw].(string); ok {
			if len(prev) == 0 {
				at = f.ptr
			}
			prev = append(prev, s)
		}
	}
	for _, f := range nv.frags {
		s, ok := f.m[kw].(string)
		if !ok || slices.Contains(prev, s) {
			continue
		}
		if len(prev) == 0 {
			d.report(pv.at, ChangeConstraintTightened, kw+" "+displayString(s)+" added")
		} else {
			d.report(at, ChangeConstraintTightened, kw+" changed from "+displayString(prev[0])+" to "+displayString(s))
		}
	}
}

// checkMultipleOf reports multipleOf values in next that don't divide one of
// prev's: every multiple of 4 is a multiple of 2, but not of 3.
func (d *differ) checkMultipleOf(pv, nv schemaView) {
	type multiple struct {
		value jsonDecimal
		text  string
		at    *schemaPointer
	}
	collect := func(v schemaView) []multiple {
		var out []multiple
		for _, f := range v.frags {
			if n, ok := f.m["multipleOf"].(json.Number); ok {
				if value, ok := parseJSONDecimal(string(n)); ok {
					out = append(out, multiple{value: value, text: string(n), at: f.ptr})
				}
			}
		}
		return out
	}
	prev := collect(pv)
	for _, n := range collect(nv) {
		if slices.ContainsFunc(prev, func(p multiple) bool { return isMultipleOf(p.value, n.value) }) {
			continue
		}
		if len(prev) == 0 {
			d.report(pv.at, ChangeConstraintTightened, "multipleOf "+truncateDetail(n.text)+" added")
		} else {
			d.report(prev[0].at, ChangeConstraintTightened,
				"multipleOf changed from "+truncateDetail(prev[0].text)+" to "+truncateDetail(n.text))
		}
	}
}

// compositeKeywords are compared by equality: any difference is reported.
// allOf is only compared this way when its branches couldn't be merged.
var compositeKeywords = []string{
	"allOf", "anyOf", "oneOf", "not", "if", "then", "else",
	"dependentSchemas", "patternProperties", "propertyNames",
	"prefixItems", "contains", "minContains", "maxContains",
	"unevaluatedProperties", "unevaluatedItems", "$dynamicRef",
}

func (d *differ) checkComposites(pv, nv schemaView) {
	for _, kw := range compositeKeywords {
		prev, at := d.compositeForms(sidePrev, pv, kw)
		next, _ := d.compositeForms(sideNext, nv, kw)
		if (len(prev) == 0 && len(next) == 0) || d.sameForms(prev, next) {
			continue
		}
		switch {
		case len(prev) == 0:
			d.report(pv.at, ChangeCompositeChanged, kw+" added")
		case len(next) == 0:
			d.report(at, ChangeCompositeChanged, kw+" removed")
		default:
			d.report(at, ChangeCompositeChanged, kw+" changed")
		}
	}
}

// compositeForms returns the canonical forms of keyword kw across a view,
// sorted so fragment order doesn't matter, and where the first one is.
func (d *differ) compositeForms(s docSide, v schemaView, kw string) ([]canonForm, *schemaPointer) {
	var forms []canonForm
	var at *schemaPointer
	for _, f := range v.frags {
		if _, ok := f.m[kw]; !ok || (kw == "allOf" && f.mergedAllOf) {
			continue
		}
		if len(forms) == 0 {
			at = f.ptr
		}
		forms = append(forms, d.keywordForm(s, f, kw))
	}
	slices.SortFunc(forms, func(a, b canonForm) int { return strings.Compare(a.text, b.text) })
	return forms, at
}

// keywordForm returns the canonical form of one fragment's keyword, once per
// fragment and keyword.
func (d *differ) keywordForm(s docSide, f schemaFragment, kw string) canonForm {
	key := canonKey{side: s, id: nodeID(f.m), keyword: kw}
	if c, ok := d.canonical[key]; ok {
		return c
	}
	var w canonWriter
	c := canonForm{text: string(w.keyword(nil, kw, f.m[kw])), refs: w.refs}
	d.spend(w.nodes)
	d.canonical[key] = c
	return c
}

// sameForms reports whether two lists of canonical forms are equal,
// including the targets of the $refs inside them.
func (d *differ) sameForms(prev, next []canonForm) bool {
	if len(prev) != len(next) {
		return false
	}
	var refs []string
	for i := range prev {
		if prev[i].text != next[i].text {
			return false
		}
		refs = append(refs, prev[i].refs...)
	}
	return d.sameRefTargets(refs)
}

// sameEntries reports whether two conjunctions are equal schema by schema.
func (d *differ) sameEntries(prev, next []schemaEntry) bool {
	if len(prev) != len(next) {
		return false
	}
	var pw, nw canonWriter
	for i := range prev {
		if !bytes.Equal(pw.schema(nil, prev[i].node), nw.schema(nil, next[i].node)) {
			return false
		}
	}
	d.spend(pw.nodes + nw.nodes)
	return d.sameRefTargets(pw.refs)
}

// sameRefTargets reports whether every reference in refs, and every
// reference reachable from their targets, resolves to equal schemas in both
// documents. Both failing to resolve counts as equal.
func (d *differ) sameRefTargets(refs []string) bool {
	queue := slices.Clone(refs)
	seen := make(map[string]struct{}, len(refs))
	for i := 0; i < len(queue); i++ {
		ref := queue[i]
		if _, dup := seen[ref]; dup {
			continue
		}
		seen[ref] = struct{}{}
		if !d.spend(1) {
			// The topic is reported as too complex instead.
			return true
		}
		c := d.refCheck(ref)
		if !c.same {
			return false
		}
		queue = append(queue, c.refs...)
	}
	return true
}

func (d *differ) refCheck(ref string) refCheck {
	if c, ok := d.refChecks[ref]; ok {
		return c
	}
	prev, prevOK := d.resolve(sidePrev, ref)
	next, nextOK := d.resolve(sideNext, ref)
	c := refCheck{same: prevOK == nextOK}
	if prevOK && nextOK {
		var pw, nw canonWriter
		c.same = bytes.Equal(pw.schema(nil, prev.node), nw.schema(nil, next.node))
		c.refs = pw.refs
		d.spend(pw.nodes + nw.nodes)
	}
	d.refChecks[ref] = c
	return c
}

// checkFilters reports top-level properties that were subscription arguments
// in prev and aren't in next: hidden with x-mcp-filter: false, no longer
// scalar, or moved out of the root properties. Removed properties are
// reported as such by the main walk.
func (d *differ) checkFilters() {
	prevRoot, _ := d.roots[sidePrev].(map[string]any)
	prevProps, _ := prevRoot["properties"].(map[string]any)
	if len(prevProps) == 0 {
		return
	}
	nextRoot, _ := d.roots[sideNext].(map[string]any)
	nextProps, _ := nextRoot["properties"].(map[string]any)
	var merged map[string]struct{}
	for _, name := range slices.Sorted(maps.Keys(prevProps)) {
		if _, ok := filterableArgument(d.roots[sidePrev], name, prevProps[name]); !ok {
			continue
		}
		at := (*schemaPointer)(nil).child("properties", name)
		nextProp, ok := nextProps[name]
		if !ok {
			if merged == nil {
				merged = d.rootPropertyNames()
			}
			if _, ok := merged[name]; ok {
				d.report(at, ChangeFilterHidden, "property no longer filterable: not declared in the root properties")
			}
			continue
		}
		if _, ok := filterableArgument(d.roots[sideNext], name, nextProp); ok {
			continue
		}
		detail := "property no longer filterable: not a scalar type"
		if node, ok := typedNode(d.roots[sideNext], nextProp); hiddenFromFilter(nextProp) || (ok && hiddenFromFilter(node)) {
			detail = "property no longer filterable: x-mcp-filter is false"
		}
		d.report(at, ChangeFilterHidden, detail)
	}
}

// rootPropertyNames returns the properties next's root declares, including
// through $refs and merged allOf branches.
func (d *differ) rootPropertyNames() map[string]struct{} {
	names := map[string]struct{}{}
	v := d.expand(sideNext, []schemaEntry{{node: d.roots[sideNext]}}, nil)
	for _, f := range v.frags {
		if props, ok := f.m["properties"].(map[string]any); ok {
			for name := range props {
				names[name] = struct{}{}
			}
		}
	}
	return names
}

var annotationKeywords = map[string]bool{
	"$schema": true, "$id": true, "$anchor": true, "$dynamicAnchor": true,
	"$vocabulary": true, "$comment": true, "$defs": true, "definitions": true,
	"title": true, "description": true, "default": true, "examples": true,
	"deprecated": true, "readOnly": true, "writeOnly": true,
	"contentEncoding": true, "contentMediaType": true, "contentSchema": true,
}

// isAnnotation reports whether a keyword never changes which values
// validate. Extensions (x-*) count: x-mcp-filter matters only for top-level
// properties, which checkFilters handles.
func isAnnotation(kw string) bool {
	return annotationKeywords[kw] || strings.HasPrefix(kw, "x-")
}

// constraintKeywords reject values by themselves. Subschema keywords that
// constrains walks into ($ref, allOf, properties, items,
// additionalProperties) aren't listed.
var constraintKeywords = func() map[string]bool {
	m := map[string]bool{
		"type": true, "enum": true, "const": true,
		"pattern": true, "format": true, "multipleOf": true,
	}
	for _, k := range boundKeywords {
		m[k.inclusive] = true
		if k.exclusive != "" {
			m[k.exclusive] = true
		}
	}
	for _, k := range compositeKeywords {
		if k != "allOf" {
			m[k] = true
		}
	}
	return m
}()

// Keywords whose values are subschemas, for canonical forms.
var (
	canonSubschemaKeywords = map[string]bool{
		"items": true, "additionalProperties": true, "additionalItems": true,
		"contains": true, "not": true, "if": true, "then": true, "else": true,
		"propertyNames": true, "unevaluatedProperties": true, "unevaluatedItems": true,
	}
	canonSubschemaMapKeywords = map[string]bool{
		"properties": true, "patternProperties": true, "dependentSchemas": true,
	}
	canonSubschemaListKeywords = map[string]bool{
		"allOf": true, "anyOf": true, "oneOf": true, "prefixItems": true,
	}
)

// canonWriter writes the canonical form schema equality is decided on:
// object keys sorted, numbers normalized so 1 and 1.0 are equal, and
// annotations left out since they don't change which payloads validate.
// $refs aren't followed; they're collected so their targets get compared
// too. nodes counts the values written, for the work budget.
type canonWriter struct {
	refs  []string
	nodes int
}

func (w *canonWriter) schema(b []byte, node any) []byte {
	m, ok := node.(map[string]any)
	if !ok {
		return w.value(b, node)
	}
	w.nodes++
	b = append(b, '{')
	first := true
	for _, k := range slices.Sorted(maps.Keys(m)) {
		if isAnnotation(k) {
			continue
		}
		if !first {
			b = append(b, ',')
		}
		first = false
		b = strconv.AppendQuote(b, k)
		b = append(b, ':')
		b = w.keyword(b, k, m[k])
	}
	return append(b, '}')
}

func (w *canonWriter) keyword(b []byte, kw string, v any) []byte {
	switch {
	case kw == "$ref":
		if ref, ok := v.(string); ok {
			w.refs = append(w.refs, ref)
		}
	case canonSubschemaKeywords[kw]:
		return w.schema(b, v)
	case canonSubschemaMapKeywords[kw]:
		if m, ok := v.(map[string]any); ok {
			b = append(b, '{')
			for i, name := range slices.Sorted(maps.Keys(m)) {
				if i > 0 {
					b = append(b, ',')
				}
				b = strconv.AppendQuote(b, name)
				b = append(b, ':')
				b = w.schema(b, m[name])
			}
			return append(b, '}')
		}
	case canonSubschemaListKeywords[kw]:
		if list, ok := v.([]any); ok {
			b = append(b, '[')
			for i, s := range list {
				if i > 0 {
					b = append(b, ',')
				}
				b = w.schema(b, s)
			}
			return append(b, ']')
		}
	}
	return w.value(b, v)
}

// value writes a JSON value with sorted keys and normalized numbers.
func (w *canonWriter) value(b []byte, v any) []byte {
	w.nodes++
	switch x := v.(type) {
	case nil:
		return append(b, "null"...)
	case bool:
		return strconv.AppendBool(b, x)
	case string:
		return strconv.AppendQuote(b, x)
	case json.Number:
		if n, ok := parseJSONDecimal(string(x)); ok {
			return append(b, n.key()...)
		}
		return append(b, x...)
	case []any:
		b = append(b, '[')
		for i, e := range x {
			if i > 0 {
				b = append(b, ',')
			}
			b = w.value(b, e)
		}
		return append(b, ']')
	case map[string]any:
		b = append(b, '{')
		for i, k := range slices.Sorted(maps.Keys(x)) {
			if i > 0 {
				b = append(b, ',')
			}
			b = strconv.AppendQuote(b, k)
			b = append(b, ':')
			b = w.value(b, x[k])
		}
		return append(b, '}')
	}
	return b
}

// valueKey returns the canonical form of an enum or const value.
func valueKey(v any) string {
	var w canonWriter
	return string(w.value(nil, v))
}

// displayValue renders a schema value for a change detail.
func displayValue(v any) string {
	raw, err := marshalNoEscape(v)
	if err != nil {
		return "?"
	}
	return truncateDetail(string(raw))
}

func displayString(s string) string {
	raw, _ := marshalNoEscape(s)
	return truncateDetail(string(raw))
}

func truncateDetail(s string) string {
	if len(s) <= maxDetailValue {
		return s
	}
	cut := maxDetailValue
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

// maxDecimalExp clamps exponents so absurd numbers still order correctly
// against ordinary ones without overflowing.
const maxDecimalExp = 1 << 60

// jsonDecimal is a JSON number as ±0.digits × 10^exp, with no leading or
// trailing zeros in digits, so equal values have equal fields. Zero has no
// digits. Comparing these needs no big-number arithmetic, so a huge exponent
// in a schema costs nothing.
type jsonDecimal struct {
	neg    bool
	digits string
	exp    int64
}

func parseJSONDecimal(s string) (jsonDecimal, bool) {
	var d jsonDecimal
	if rest, ok := strings.CutPrefix(s, "-"); ok {
		d.neg, s = true, rest
	}
	mantissa, expText := s, ""
	if i := strings.IndexAny(s, "eE"); i >= 0 {
		mantissa, expText = s[:i], s[i+1:]
	}
	intPart, frac, _ := strings.Cut(mantissa, ".")
	if intPart == "" || !isDigits(intPart) || !isDigits(frac) {
		return jsonDecimal{}, false
	}
	var exp int64
	if expText != "" {
		e, err := strconv.ParseInt(expText, 10, 64)
		if err != nil && !errors.Is(err, strconv.ErrRange) {
			return jsonDecimal{}, false
		}
		exp = min(max(e, -maxDecimalExp), maxDecimalExp)
	}
	digits := intPart + frac
	exp += int64(len(intPart))
	trimmed := strings.TrimLeft(digits, "0")
	exp -= int64(len(digits) - len(trimmed))
	d.digits = strings.TrimRight(trimmed, "0")
	if d.digits == "" {
		return jsonDecimal{}, true
	}
	d.exp = exp
	return d, true
}

func isDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func (a jsonDecimal) sign() int {
	switch {
	case a.digits == "":
		return 0
	case a.neg:
		return -1
	}
	return 1
}

func (a jsonDecimal) cmp(b jsonDecimal) int {
	if c := cmp.Compare(a.sign(), b.sign()); c != 0 || a.digits == "" {
		return c
	}
	// Same sign: compare magnitudes, then flip for negatives.
	c := cmp.Compare(a.exp, b.exp)
	if c == 0 {
		// Digits have no trailing zeros, so string order is numeric order.
		c = strings.Compare(a.digits, b.digits)
	}
	if a.neg {
		return -c
	}
	return c
}

// key is a canonical text form: equal numbers get equal keys.
func (a jsonDecimal) key() string {
	if a.digits == "" {
		return "0"
	}
	sign := ""
	if a.neg {
		sign = "-"
	}
	return sign + a.digits + "e" + strconv.FormatInt(a.exp, 10)
}

// maxMultipleShift bounds the big-integer work isMultipleOf does; beyond it
// a multipleOf change is reported rather than computed.
const maxMultipleShift = 1000

// isMultipleOf reports whether a is an integer multiple of b, for positive a
// and b, so every multiple of a is a multiple of b.
func isMultipleOf(a, b jsonDecimal) bool {
	if a.sign() <= 0 || b.sign() <= 0 {
		return false
	}
	// value = digits × 10^(exp - len(digits)) with integer digits.
	shift := (a.exp - int64(len(a.digits))) - (b.exp - int64(len(b.digits)))
	if shift > maxMultipleShift || shift < -maxMultipleShift ||
		len(a.digits) > maxMultipleShift || len(b.digits) > maxMultipleShift {
		return false
	}
	x, _ := new(big.Int).SetString(a.digits, 10)
	y, _ := new(big.Int).SetString(b.digits, 10)
	pow := new(big.Int).Exp(big.NewInt(10), big.NewInt(max(shift, -shift)), nil)
	if shift >= 0 {
		x.Mul(x, pow)
	} else {
		y.Mul(y, pow)
	}
	return new(big.Int).Rem(x, y).Sign() == 0
}
