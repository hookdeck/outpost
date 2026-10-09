package destmcp

import (
	"fmt"
	"strings"
	"text/template"
)

// InstructionsData is what the mcp instructions template gets, as plain
// values: RenderInstructions formats each as a markdown inline code span
// before the template sees it.
type InstructionsData struct {
	// ServerURL is MCP_SERVER_URL, or "" when unset.
	ServerURL string
	// Topics are the MCP-enabled topics, in TOPICS order.
	Topics []string
}

// instructionsValues is the template's view of InstructionsData: the same
// fields, each already a code span.
type instructionsValues struct {
	ServerURL string
	Topics    []string
}

// RenderInstructions renders the mcp type's instructions template (its
// metadata Instructions, a Go text/template) into markdown. The result is
// static for a process: render it once.
func RenderInstructions(tmpl string, data InstructionsData) (string, error) {
	t, err := parseInstructions(tmpl)
	if err != nil {
		return "", err
	}
	return executeInstructions(t, data)
}

func parseInstructions(tmpl string) (*template.Template, error) {
	t, err := template.New("instructions").Option("missingkey=error").Parse(tmpl)
	if err != nil {
		return nil, fmt.Errorf("mcp instructions template: %w", err)
	}
	return t, nil
}

func executeInstructions(t *template.Template, data InstructionsData) (string, error) {
	values := instructionsValues{ServerURL: codeSpan(data.ServerURL)}
	for _, topic := range data.Topics {
		values.Topics = append(values.Topics, codeSpan(topic))
	}
	var b strings.Builder
	if err := t.Execute(&b, values); err != nil {
		return "", fmt.Errorf("mcp instructions template: %w", err)
	}
	return b.String(), nil
}

// codeSpan formats s as a CommonMark inline code span that s can't break
// out of: control characters (line breaks included) become spaces, and the
// fence is one backtick longer than the longest backtick run in s, padded
// with spaces when s starts or ends with a backtick. "" stays "", so
// templates can test for it.
func codeSpan(s string) string {
	if s == "" {
		return ""
	}
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, s)
	longest, run := 0, 0
	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	fence := strings.Repeat("`", longest+1)
	if strings.HasPrefix(s, "`") || strings.HasSuffix(s, "`") {
		return fence + " " + s + " " + fence
	}
	return fence + s + fence
}
