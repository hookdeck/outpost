module github.com/hookdeck/outpost/sdks/outpost-go/mcpevents/mcpsdk

go 1.26.8

require (
	github.com/hookdeck/outpost/sdks/outpost-go v1.7.0
	github.com/modelcontextprotocol/go-sdk v1.8.0
	github.com/stretchr/testify v1.12.1
)

require (
	github.com/google/jsonschema-go v0.4.3 // indirect
	github.com/segmentio/asm v1.1.3 // indirect
	github.com/segmentio/encoding v0.5.4 // indirect
	github.com/yosida95/uritemplate/v3 v3.0.2 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/oauth2 v0.35.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.41.0 // indirect
	golang.org/x/time v0.15.0 // indirect
)

// Builds against the SDK in this repository. Modules that depend on this one
// ignore it and use the outpost-go version they require.
replace github.com/hookdeck/outpost/sdks/outpost-go => ../..
