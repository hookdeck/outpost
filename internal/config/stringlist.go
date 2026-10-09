package config

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// StringList is a list config value: a comma-separated string in the
// environment, and in YAML a sequence or a comma-separated string, so a single
// entry can be written without brackets. Entries are trimmed and empty entries
// dropped. A YAML null empties it, like any YAML list.
type StringList []string

func (l *StringList) UnmarshalText(b []byte) error {
	*l = splitList(strings.Split(string(b), ","))
	return nil
}

func (l *StringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		return l.UnmarshalText([]byte(node.Value))
	case yaml.SequenceNode:
		var items []string
		if err := node.Decode(&items); err != nil {
			return err
		}
		*l = splitList(items)
		return nil
	default:
		return fmt.Errorf("line %d: a list must be a sequence or a comma-separated string", node.Line)
	}
}

func splitList(items []string) StringList {
	var list StringList
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			list = append(list, item)
		}
	}
	return list
}
