// Package mcpcheck runs a conformance flow against an MCP Events server
// that offers webhook delivery and reports one result per rule.
package mcpcheck

import (
	"fmt"
	"strings"
)

// Status is the outcome of one check.
type Status string

const (
	Pass Status = "PASS"
	Fail Status = "FAIL"
	Warn Status = "WARN"
	Skip Status = "SKIP"
)

// Result is one check outcome. Section names the SEP-3415 section behind it.
type Result struct {
	ID      string `json:"id"`
	Section string `json:"section"`
	Status  Status `json:"status"`
	Detail  string `json:"detail,omitempty"`
}

// Report lists the results in flow order.
type Report []Result

// Failed reports whether any check failed.
func (r Report) Failed() bool {
	for _, x := range r {
		if x.Status == Fail {
			return true
		}
	}
	return false
}

// Count returns the number of results with status s.
func (r Report) Count(s Status) int {
	n := 0
	for _, x := range r {
		if x.Status == s {
			n++
		}
	}
	return n
}

const publishOnlyDetail = "publish mode only (--publish-tenant)"

func (r Report) String() string {
	width := 0
	for _, x := range r {
		width = max(width, len(x.ID))
	}
	var b strings.Builder
	needPublish := 0
	for _, x := range r {
		fmt.Fprintf(&b, "%s  %-*s", x.Status, width, x.ID)
		if x.Detail != "" {
			b.WriteString("  " + x.Detail)
		}
		b.WriteByte('\n')
		if x.Status == Skip && x.Detail == publishOnlyDetail {
			needPublish++
		}
	}
	fmt.Fprintf(&b, "\n%d passed, %d failed, %d warnings, %d skipped\n", r.Count(Pass), r.Count(Fail), r.Count(Warn), r.Count(Skip))
	if needPublish > 0 {
		fmt.Fprintf(&b, "%d checks need publish mode; delivered events were shape-checked only\n", needPublish)
	}
	return b.String()
}
