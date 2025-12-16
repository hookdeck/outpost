package simplejsonmatch

// Match checks if the input JSON matches the given schema.
// Returns true if the input matches the schema, false otherwise.
func Match(input, schema any) bool {
	return matchJSONToSchema(input, schema)
}

// matchJSONToSchema is the internal recursive matching function.
func matchJSONToSchema(input, schema any) bool {
	defer func() {
		// Recover from any panics and return false
		if r := recover(); r != nil {
			// Silently fail on panic, matching JS behavior of returning false on errors
		}
	}()

	// Handle $not at the top level
	if schemaMap, ok := toMap(schema); ok {
		if notSchema, hasNot := schemaMap[OpNot]; hasNot {
			result := matchJSONToSchema(input, notSchema)

			// If $not is the only key, just negate the result
			if len(schemaMap) == 1 {
				return !result
			}

			// If the negated condition matches (result is true), the whole schema fails
			if result {
				return false
			}

			// Otherwise, continue checking other conditions without $not
			// but we need to process them as well
		}
	}

	// Handle primitives and arrays at the input level
	if isPrimitive(input) || isArray(input) {
		return !recursivelyMatchValue(input, schema)
	}

	// Handle object schema matching
	if schemaMap, ok := toMap(schema); ok {
		inputMap, inputIsMap := toMap(input)
		if !inputIsMap {
			return !recursivelyMatchValue(input, schema)
		}

		// Check each key in the schema
		for key, subSchema := range schemaMap {
			// Skip $not as it was handled above
			if key == OpNot {
				continue
			}

			// Handle $or at any level
			if key == OpOr {
				if orSchemas, ok := toSlice(subSchema); ok {
					matched := false
					for _, orSchema := range orSchemas {
						if matchJSONToSchema(input, orSchema) {
							matched = true
							break
						}
					}
					if !matched {
						return false
					}
					continue
				}
			}

			// Handle $and at any level
			if key == OpAnd {
				if andSchemas, ok := toSlice(subSchema); ok {
					for _, andSchema := range andSchemas {
						if !matchJSONToSchema(input, andSchema) {
							return false
						}
					}
					continue
				}
			}

			// Get the value for this key (may be undefined)
			inputValue, exists := inputMap[key]
			if !exists {
				// Handle $exist: false case
				if subSchemaMap, ok := toMap(subSchema); ok {
					if existVal, hasExist := subSchemaMap[OpExist]; hasExist {
						if existBool, ok := existVal.(bool); ok && !existBool {
							// $exist: false and key doesn't exist - this condition passes
							continue
						}
					}
				}
				// Key doesn't exist and no $exist: false - fail
				return false
			}

			// Recursively match the value
			if recursivelyMatchValue(inputValue, subSchema) {
				return false
			}
		}
		return true
	}

	return !recursivelyMatchValue(input, schema)
}

// recursivelyMatchValue checks if a value matches a schema pattern.
// Returns true if there's a MISMATCH (inverted logic for internal use).
func recursivelyMatchValue(input, schema any) bool {
	// Handle primitive schema
	if isPrimitive(schema) {
		if isPrimitive(input) {
			// Direct comparison
			return !compareEquality(input, schema)
		}
		if inputSlice, ok := toSlice(input); ok {
			// Check if any element in the array matches
			for _, v := range inputSlice {
				if !recursivelyMatchValue(v, schema) {
					return false // Found a match
				}
			}
			return true // No match found
		}
		if _, ok := toMap(input); ok {
			return true // Object doesn't match primitive
		}
	}

	// Handle array input
	if inputSlice, ok := toSlice(input); ok {
		// If schema is also an array, check contains logic
		if schemaSlice, ok := toSlice(schema); ok {
			for _, subSchema := range schemaSlice {
				found := false
				for _, arrayItem := range inputSlice {
					if !recursivelyMatchValue(arrayItem, subSchema) {
						found = true
						break
					}
				}
				if !found {
					return true // Schema element not found in array
				}
			}
			return false // All schema elements found
		}

		// Check if schema has operators
		if schemaMap, ok := toMap(schema); ok {
			hasOperators := false
			for key := range schemaMap {
				if isOperatorKey(key) {
					hasOperators = true
					break
				}
			}
			if hasOperators {
				// Apply operators to the array
				for key, value := range schemaMap {
					if isOperatorKey(key) {
						result, err := applyOperator(key, input, value)
						if err != nil || !result {
							return true
						}
					}
				}
				return false
			}
		}

		// Check each array element against the schema
		for _, v := range inputSlice {
			if !recursivelyMatchValue(v, schema) {
				return false // Found a match
			}
		}
		return true // No match found
	}

	// Handle object schema
	if schemaMap, ok := toMap(schema); ok {
		// Handle $or
		if orSchemas, hasOr := schemaMap[OpOr]; hasOr {
			if orSlice, ok := toSlice(orSchemas); ok {
				for _, condSchema := range orSlice {
					if matchJSONToSchema(input, condSchema) {
						return false // Found a match
					}
				}
				return true // No match in $or
			}
		}

		// Check for operators in schema
		operators := make(map[string]any)
		for key, value := range schemaMap {
			if isOperatorKey(key) {
				operators[key] = value
			}
		}

		if len(operators) > 0 {
			for op, compareValue := range operators {
				result, err := applyOperator(op, input, compareValue)
				if err != nil || !result {
					return true // Operator failed
				}
			}
			return false // All operators passed
		}

		// No operators - treat as nested object match
		if isPrimitive(input) {
			return true // Can't match object schema against primitive
		}

		return !matchJSONToSchema(input, schema)
	}

	return true
}

// isArray checks if a value is an array type.
func isArray(v any) bool {
	_, ok := toSlice(v)
	return ok
}
