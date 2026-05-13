module github.com/hookdeck/outpost/spec-sdk-tests/tests/destinations

go 1.22

replace github.com/hookdeck/outpost/sdks/outpost-go => ../../../sdks/outpost-go

require (
	github.com/hookdeck/outpost/sdks/outpost-go v0.0.0-00010101000000-000000000000
	github.com/stretchr/testify v1.11.1
)

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/spyzhov/ajson v0.8.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
