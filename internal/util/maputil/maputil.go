package maputil

// MergeStringMaps merges two string maps, with input taking precedence over original.
// This is useful for partial updates where we want to merge new values with existing ones.
func MergeStringMaps(original, input map[string]string) map[string]string {
	merged := make(map[string]string)
	for k, v := range original {
		merged[k] = v
	}
	for k, v := range input {
		merged[k] = v
	}
	return merged
}
