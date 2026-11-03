package destmcp

import "time"

// SetNow replaces the provider's clock (rotation, signing, obfuscation).
func SetNow(p *Provider, now func() time.Time) {
	p.now = now
}

const MaxDrainBytes = maxDrainBytes
