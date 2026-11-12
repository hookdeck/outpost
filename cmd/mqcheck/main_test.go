package main

import (
	"io/fs"
	"testing"

	"github.com/hookdeck/outpost/internal/mqcheck/cases"
	"github.com/hookdeck/outpost/internal/mqcheck/payload"
	"github.com/hookdeck/outpost/internal/mqcheck/profile"
	"github.com/hookdeck/outpost/internal/mqcheck/provider"
)

func TestProfilesLoad(t *testing.T) {
	sub := mustSub(profilesFS, "profiles")
	names, _ := fs.Glob(sub, "*.yaml")
	if len(names) < 2 {
		t.Fatalf("profiles: %v", names)
	}
	for _, n := range names {
		p, err := profile.Load(n[:len(n)-len(".yaml")], sub)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{p.Payloads.Small, p.Payloads.Large, p.Capacity.Payload} {
			if _, err := payload.ParseSpec(s); err != nil {
				t.Errorf("%s: %v", n, err)
			}
		}
		for _, l := range p.Capacity.Large {
			if _, err := payload.ParseSpec(l.Payload); err != nil {
				t.Errorf("%s: %v", n, err)
			}
		}
	}
}

func TestRegistry(t *testing.T) {
	for _, name := range []string{"awssqs", "gcppubsub"} {
		if _, ok := provider.Lookup(name); !ok {
			t.Errorf("provider %s not registered", name)
		}
	}
	seen := map[string]bool{}
	for _, c := range cases.All() {
		if seen[c.ID] {
			t.Errorf("duplicate case id %s", c.ID)
		}
		seen[c.ID] = true
		if c.Run == nil || len(c.Req) == 0 || len(c.Tiers) == 0 {
			t.Errorf("case %s incomplete", c.ID)
		}
	}
}
