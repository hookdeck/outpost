package topicschema

// Keywords whose values are schemas, by shape. Walks over payload schemas
// descend only through these, so values of const, enum, default, examples
// and unknown keywords are never mistaken for schemas.
var (
	subschemaKeywords = map[string]bool{
		"additionalItems":       true,
		"additionalProperties":  true,
		"contains":              true,
		"contentSchema":         true,
		"else":                  true,
		"if":                    true,
		"items":                 true,
		"not":                   true,
		"propertyNames":         true,
		"then":                  true,
		"unevaluatedItems":      true,
		"unevaluatedProperties": true,
	}
	subschemaListKeywords = map[string]bool{
		"allOf":       true,
		"anyOf":       true,
		"oneOf":       true,
		"prefixItems": true,
	}
	// Values of these are objects mapping names to schemas. "dependencies"
	// also maps to arrays of names, which are skipped.
	subschemaMapKeywords = map[string]bool{
		"$defs":             true,
		"definitions":       true,
		"dependencies":      true,
		"dependentSchemas":  true,
		"patternProperties": true,
		"properties":        true,
	}
)

// visitSchemaObjects calls fn for every schema object in node, a tree from
// decodeJSON, with its dot path from root. Boolean schemas are skipped.
func visitSchemaObjects(node any, path string, fn func(obj map[string]any, path string)) {
	visitSchemas(node, path, func(schema any, path string) {
		if obj, ok := schema.(map[string]any); ok {
			fn(obj, path)
		}
	})
}

// visitSchemas calls fn for every schema in node, a tree from decodeJSON,
// boolean schemas included, with its dot path from root.
func visitSchemas(node any, path string, fn func(schema any, path string)) {
	obj, ok := node.(map[string]any)
	if !ok {
		if _, ok := node.(bool); ok {
			fn(node, path)
		}
		return
	}
	fn(obj, path)
	for key, value := range obj {
		switch {
		case subschemaKeywords[key]:
			// items may still be an array in schemas written for older drafts;
			// the metaschema rejects it, but walking it is harmless.
			if list, ok := value.([]any); ok {
				for i, v := range list {
					visitSchemas(v, indexPath(appendPathKey(path, key), i), fn)
				}
				continue
			}
			visitSchemas(value, appendPathKey(path, key), fn)
		case subschemaListKeywords[key]:
			list, _ := value.([]any)
			for i, v := range list {
				visitSchemas(v, indexPath(appendPathKey(path, key), i), fn)
			}
		case subschemaMapKeywords[key]:
			m, _ := value.(map[string]any)
			for name, v := range m {
				visitSchemas(v, appendPathKey(appendPathKey(path, key), name), fn)
			}
		}
	}
}

// propertyNameSet returns every name declared under properties anywhere in
// the schema tree. Error paths show instance keys only when they are in it,
// since those strings come from the schema rather than from the instance.
func propertyNameSet(tree any) map[string]struct{} {
	names := map[string]struct{}{}
	visitSchemaObjects(tree, "", func(obj map[string]any, _ string) {
		if props, ok := obj["properties"].(map[string]any); ok {
			for name := range props {
				names[name] = struct{}{}
			}
		}
	})
	return names
}
