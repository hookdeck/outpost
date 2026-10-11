package metadata

import (
	"regexp"
	"slices"
)

// Field returns the config or credential field with the given key.
func (m *ProviderMetadata) Field(key string) (FieldSchema, bool) {
	for _, fields := range [][]FieldSchema{m.ConfigFields, m.CredentialFields} {
		for _, f := range fields {
			if f.Key == key {
				return f, true
			}
		}
	}
	return FieldSchema{}, false
}

// FieldValue returns the value of the field key from config, then
// credentials, falling back to the field's default.
func (m *ProviderMetadata) FieldValue(key string, config, credentials map[string]string) string {
	if v := config[key]; v != "" {
		return v
	}
	if v := credentials[key]; v != "" {
		return v
	}
	if f, ok := m.Field(key); ok && f.Default != nil {
		return *f.Default
	}
	return ""
}

// IsVisible reports whether field applies to a destination with the given
// config and credentials.
func (m *ProviderMetadata) IsVisible(field FieldSchema, config, credentials map[string]string) bool {
	if field.VisibleWhen == nil {
		return true
	}
	return slices.Contains(field.VisibleWhen.Values, m.FieldValue(field.VisibleWhen.Key, config, credentials))
}

// IsController reports whether another field's visibility depends on key.
func (m *ProviderMetadata) IsController(key string) bool {
	for _, fields := range [][]FieldSchema{m.ConfigFields, m.CredentialFields} {
		for _, f := range fields {
			if f.VisibleWhen != nil && f.VisibleWhen.Key == key {
				return true
			}
		}
	}
	return false
}

// WithoutOption returns a copy of m with option value removed from the select
// field key, along with the fields and instructions sections only visible for
// that option. When a single option remains, the select itself is removed and
// the fields that depended on it become unconditional; the remaining option is
// returned as fixed (empty otherwise).
func (m *ProviderMetadata) WithoutOption(key, value string) (result *ProviderMetadata, fixed string) {
	out := *m
	out.ConfigFields = slices.Clone(m.ConfigFields)
	out.CredentialFields = slices.Clone(m.CredentialFields)

	var remaining []FieldOption
	for _, fields := range []*[]FieldSchema{&out.ConfigFields, &out.CredentialFields} {
		for i, f := range *fields {
			if f.Key == key {
				f.Options = slices.DeleteFunc(slices.Clone(f.Options), func(o FieldOption) bool { return o.Value == value })
				remaining = f.Options
				(*fields)[i] = f
			}
		}
	}
	collapse := len(remaining) == 1
	if collapse {
		fixed = remaining[0].Value
	}

	for _, fields := range []*[]FieldSchema{&out.ConfigFields, &out.CredentialFields} {
		*fields = slices.DeleteFunc(*fields, func(f FieldSchema) bool {
			return collapse && f.Key == key
		})
		for i, f := range *fields {
			if f.VisibleWhen == nil || f.VisibleWhen.Key != key {
				continue
			}
			values := slices.DeleteFunc(slices.Clone(f.VisibleWhen.Values), func(v string) bool { return v == value })
			switch {
			case len(values) == 0:
				f.VisibleWhen = &FieldCondition{Key: key}
			case collapse:
				f.VisibleWhen = nil
			default:
				f.VisibleWhen = &FieldCondition{Key: key, Values: values}
			}
			(*fields)[i] = f
		}
		*fields = slices.DeleteFunc(*fields, func(f FieldSchema) bool {
			return f.VisibleWhen != nil && f.VisibleWhen.Key == key && len(f.VisibleWhen.Values) == 0
		})
	}
	out.Instructions = removeInstructionSection(out.Instructions, key, value)
	return &out, fixed
}

// removeInstructionSection drops the instructions content marked as only
// applying to key=value:
//
//	<!-- visible_when key=value -->
//	...
//	<!-- /visible_when -->
func removeInstructionSection(instructions, key, value string) string {
	re := regexp.MustCompile(`(?s)<!-- visible_when ` + regexp.QuoteMeta(key+"="+value) + ` -->.*?<!-- /visible_when -->\n*`)
	return re.ReplaceAllString(instructions, "")
}
