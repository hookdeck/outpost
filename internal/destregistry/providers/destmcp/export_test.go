package destmcp

import "time"

// SetNow replaces the provider's clock (rotation, signing, obfuscation).
func SetNow(p *Provider, now func() time.Time) {
	p.now = now
}

// SetDeliveryTimeout replaces the 10s attempt timeout, so tests can see the
// registry apply it without waiting 10s.
func SetDeliveryTimeout(p *Provider, d time.Duration) {
	p.deliveryTimeout = d
}

// SetHostWait replaces how long an attempt waits for a callback host slot
// (at most half the attempt's remaining time either way).
func SetHostWait(p *Provider, d time.Duration) {
	p.hostWait = d
}

// CodeSpan exposes codeSpan.
var CodeSpan = codeSpan

const MaxDrainBytes = maxDrainBytes
