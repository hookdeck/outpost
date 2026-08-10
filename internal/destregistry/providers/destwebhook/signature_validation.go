package destwebhook

import (
	"fmt"
	"time"
)

// dryRunFormatters renders both formatters against synthetic payloads, so a
// bad template fails provider construction instead of the first delivery.
//
// Together with the parse step in New, this catches two classes of failure:
// templates that don't parse, and templates that parse but reference a field
// the payload type doesn't have (the content and header templates render
// against different types, so borrowing a field from the wrong one is valid
// syntax). Field resolution depends on the payload's type rather than its
// values, so a synthetic payload settles that class.
//
// It is a smoke test, not a proof: value-dependent failures — `{{index
// .Signatures 2}}` with three secrets configured, or a helper that errors only
// on particular input — can still fail at delivery. Those degrade to a failed
// attempt via the error returned by Format.
func dryRunFormatters(sig SignatureFormatter, header HeaderFormatter, contentTemplate, headerTemplate string) error {
	now := time.Now()

	if _, err := sig.Format(SignaturePayload{
		EventID:   "evt_validation",
		Topic:     "validation.topic",
		Timestamp: now,
		Body:      `{"validation":true}`,
	}); err != nil {
		return fmt.Errorf("invalid signature content template %q: %w", contentTemplate, err)
	}

	// Two signatures, since rotation is when .Signatures holds more than one
	// element and when a template that mishandles the list shows it.
	if _, err := header.Format(HeaderPayload{
		EventID:    "evt_validation",
		Topic:      "validation.topic",
		Timestamp:  now,
		Signatures: []string{"sig_current", "sig_previous"},
	}); err != nil {
		return fmt.Errorf("invalid signature header template %q: %w", headerTemplate, err)
	}

	return nil
}
