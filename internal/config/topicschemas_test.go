package config_test

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/hookdeck/outpost/internal/config"
	"github.com/hookdeck/outpost/internal/topicschema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const orderSchemasJSON = `{"order.created":{"description":"An order was placed.","validation":"enforce","payload_schema":{"type":"object","properties":{"total":{"type":"number"}},"required":["total"]}}}`

const orderSchemasYAML = `order.created:
  description: An order was placed.
  validation: enforce
  payload_schema:
    type: object
    properties:
      total:
        type: number
    required: [total]
`

// recordingOS is a mockOS that records every file read.
type recordingOS struct {
	*mockOS
	reads []string
}

func (r *recordingOS) ReadFile(name string) ([]byte, error) {
	r.reads = append(r.reads, name)
	return r.mockOS.ReadFile(name)
}

func newTopicsOS(env map[string]string, files map[string]string) *recordingOS {
	m := &mockOS{files: map[string][]byte{}, envVars: map[string]string{"TOPICS": "order.created,order.updated"}}
	for k, v := range env {
		m.envVars[k] = v
	}
	for k, v := range files {
		m.files[k] = []byte(v)
	}
	return &recordingOS{mockOS: m}
}

// parseTopicsConfig parses config from env and files, with a YAML config file
// when yamlConfig is set.
func parseTopicsConfig(t *testing.T, env map[string]string, files map[string]string, yamlConfig string) (*config.Config, *recordingOS) {
	t.Helper()
	osi := newTopicsOS(env, files)
	if yamlConfig != "" {
		osi.files["config.yaml"] = []byte(yamlConfig)
		osi.envVars["CONFIG"] = "config.yaml"
	}
	cfg, err := config.ParseWithoutValidation(config.Flags{}, osi)
	require.NoError(t, err)
	return cfg, osi
}

func loadCatalog(t *testing.T, cfg *config.Config) *topicschema.Catalog {
	t.Helper()
	catalog, err := cfg.LoadTopicCatalog()
	require.NoError(t, err)
	require.NotNil(t, catalog)
	assert.Same(t, catalog, cfg.TopicCatalog())
	return catalog
}

func requireOrderCreated(t *testing.T, catalog *topicschema.Catalog) topicschema.Topic {
	t.Helper()
	topic, ok := catalog.Topic("order.created")
	require.True(t, ok)
	assert.Equal(t, topicschema.ValidationEnforce, topic.Validation)
	assert.Equal(t, "An order was placed.", topic.Description)
	assert.JSONEq(t, `{"type":"object","properties":{"total":{"type":"number"}},"required":["total"]}`, string(topic.PayloadSchema))
	return topic
}

func TestTopicSchemas_Sources(t *testing.T) {
	t.Run("env JSON", func(t *testing.T) {
		cfg, _ := parseTopicsConfig(t, map[string]string{"TOPICS_SCHEMAS": orderSchemasJSON}, nil, "")
		assert.True(t, cfg.TopicsSchemas.IsSet())
		requireOrderCreated(t, loadCatalog(t, cfg))
	})

	t.Run("YAML mapping", func(t *testing.T) {
		yamlConfig := "topics_schemas:\n" + indent(orderSchemasYAML, "  ")
		cfg, _ := parseTopicsConfig(t, nil, nil, yamlConfig)
		assert.True(t, cfg.TopicsSchemas.IsSet())
		requireOrderCreated(t, loadCatalog(t, cfg))
	})

	t.Run("YAML string containing JSON", func(t *testing.T) {
		quoted, err := json.Marshal(orderSchemasJSON)
		require.NoError(t, err)
		cfg, _ := parseTopicsConfig(t, nil, nil, "topics_schemas: "+string(quoted)+"\n")
		assert.JSONEq(t, orderSchemasJSON, string(cfg.TopicsSchemas.Raw()))
		requireOrderCreated(t, loadCatalog(t, cfg))
	})

	t.Run("env replaces YAML", func(t *testing.T) {
		yamlConfig := "topics_schemas:\n  order.updated:\n    description: from YAML\n"
		cfg, _ := parseTopicsConfig(t, map[string]string{"TOPICS_SCHEMAS": orderSchemasJSON}, nil, yamlConfig)
		catalog := loadCatalog(t, cfg)
		requireOrderCreated(t, catalog)
		updated, ok := catalog.Topic("order.updated")
		require.True(t, ok)
		assert.Empty(t, updated.Description)
	})

	t.Run("empty env and YAML null are unset", func(t *testing.T) {
		cfg, _ := parseTopicsConfig(t, map[string]string{"TOPICS_SCHEMAS": ""}, nil, "topics_schemas:\n")
		assert.False(t, cfg.TopicsSchemas.IsSet())
		assert.False(t, loadCatalog(t, cfg).HasSchemas())
	})

	t.Run("JSON file", func(t *testing.T) {
		cfg, osi := parseTopicsConfig(t,
			map[string]string{"TOPICS_SCHEMAS_FILE": "schemas/topics.JSON"},
			map[string]string{"schemas/topics.JSON": orderSchemasJSON}, "")
		requireOrderCreated(t, loadCatalog(t, cfg))
		assert.Contains(t, osi.reads, "schemas/topics.JSON")
	})

	t.Run("YAML file", func(t *testing.T) {
		cfg, osi := parseTopicsConfig(t,
			map[string]string{"TOPICS_SCHEMAS_FILE": "schemas/topics.yaml"},
			map[string]string{"schemas/topics.yaml": orderSchemasYAML}, "")
		requireOrderCreated(t, loadCatalog(t, cfg))
		assert.Contains(t, osi.reads, "schemas/topics.yaml")
	})

	t.Run("missing file", func(t *testing.T) {
		cfg, _ := parseTopicsConfig(t, map[string]string{"TOPICS_SCHEMAS_FILE": "missing.yaml"}, nil, "")
		_, err := cfg.LoadTopicCatalog()
		require.ErrorIs(t, err, config.ErrInvalidTopicSchemas)
		assert.Contains(t, err.Error(), "reading TOPICS_SCHEMAS_FILE")
	})

	t.Run("no schemas", func(t *testing.T) {
		cfg, _ := parseTopicsConfig(t, nil, nil, "")
		catalog := loadCatalog(t, cfg)
		assert.False(t, catalog.HasSchemas())
		assert.Len(t, catalog.Topics(), 2)
	})
}

func TestTopicSchemas_UnquotedYAMLDateStaysString(t *testing.T) {
	const schemas = `order.created:
  payload_schema:
    type: object
    properties:
      day:
        type: string
        format: date
        default: 2026-01-01
`
	wantSchema := `{"type":"object","properties":{"day":{"type":"string","format":"date","default":"2026-01-01"}}}`

	t.Run("inline", func(t *testing.T) {
		cfg, _ := parseTopicsConfig(t, nil, nil, "topics_schemas:\n"+indent(schemas, "  "))
		topic, ok := loadCatalog(t, cfg).Topic("order.created")
		require.True(t, ok)
		assert.JSONEq(t, wantSchema, string(topic.PayloadSchema))
	})

	t.Run("file", func(t *testing.T) {
		cfg, _ := parseTopicsConfig(t,
			map[string]string{"TOPICS_SCHEMAS_FILE": "topics.yml"},
			map[string]string{"topics.yml": schemas}, "")
		topic, ok := loadCatalog(t, cfg).Topic("order.created")
		require.True(t, ok)
		assert.JSONEq(t, wantSchema, string(topic.PayloadSchema))
	})
}

func TestTopicSchemas_SchemaErrors(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantMsg string
	}{
		{
			name:    "mcp enabled without payload_schema",
			env:     map[string]string{"TOPICS_SCHEMAS": `{"order.created":{"mcp":{"enabled":true}}}`},
			wantMsg: "payload_schema",
		},
		{
			name:    "key missing from TOPICS",
			env:     map[string]string{"TOPICS_SCHEMAS": `{"invoice.paid":{"description":"x"}}`},
			wantMsg: "invoice.paid",
		},
		{
			name:    "invalid JSON",
			env:     map[string]string{"TOPICS_SCHEMAS": `{"order.created":`},
			wantMsg: "invalid topic schemas",
		},
		{
			name:    "unknown field",
			env:     map[string]string{"TOPICS_SCHEMAS": `{"order.created":{"validaton":"warn"}}`},
			wantMsg: "validaton",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, _ := parseTopicsConfig(t, tt.env, nil, "")
			catalog, err := cfg.LoadTopicCatalog()
			require.Error(t, err)
			assert.Nil(t, catalog)
			assert.ErrorIs(t, err, config.ErrInvalidTopicSchemas)
			var configErr *topicschema.ConfigError
			require.ErrorAs(t, err, &configErr)
			assert.Contains(t, err.Error(), tt.wantMsg)
			assert.True(t, strings.HasPrefix(err.Error(), "invalid topic schemas:"), err.Error())
			assert.Equal(t, 1, strings.Count(err.Error(), "invalid topic schemas"), "not double-prefixed: %s", err)
			// The failed load stores nothing.
			assert.False(t, cfg.TopicCatalog().HasSchemas())
		})
	}

	// Services that never load the catalog (delivery, log, outpost migrate,
	// outpost config list) parse the same config file, so a topics_schemas
	// mapping that can't be converted only fails LoadTopicCatalog.
	yamlErrors := []struct {
		name    string
		yaml    string
		wantMsg string
	}{
		{
			name:    "alias cycle",
			yaml:    "topics_schemas: &a\n  order.created: *a\n",
			wantMsg: "alias *a refers to a node that contains it",
		},
		{
			name:    "merge key",
			yaml:    "topics_schemas:\n  order.created: &base\n    description: d\n  order.updated:\n    <<: *base\n",
			wantMsg: "merge keys (<<) are not supported",
		},
	}
	for _, tt := range yamlErrors {
		t.Run("YAML "+tt.name+" fails only the load", func(t *testing.T) {
			osi := newTopicsOS(map[string]string{
				"CONFIG":                "config.yaml",
				"SERVICE":               "delivery",
				"POSTGRES_URL":          "postgres://localhost:5432/outpost",
				"RABBITMQ_SERVER_URL":   "amqp://localhost:5672",
				"AES_ENCRYPTION_SECRET": "secret",
			}, map[string]string{"config.yaml": tt.yaml})
			cfg, err := config.ParseWithOS(config.Flags{}, osi)
			require.NoError(t, err)
			require.NoError(t, cfg.Validate(config.Flags{}))
			assert.True(t, cfg.TopicsSchemas.IsSet())

			catalog, err := cfg.LoadTopicCatalog()
			assert.Nil(t, catalog)
			require.ErrorIs(t, err, config.ErrInvalidTopicSchemas)
			assert.True(t, strings.HasPrefix(err.Error(), "invalid topic schemas: topics_schemas: "), err.Error())
			assert.Contains(t, err.Error(), tt.wantMsg)
		})
	}

	t.Run("env replaces a YAML mapping that can't be converted", func(t *testing.T) {
		cfg, _ := parseTopicsConfig(t, map[string]string{"TOPICS_SCHEMAS": orderSchemasJSON}, nil, yamlErrors[1].yaml)
		requireOrderCreated(t, loadCatalog(t, cfg))
	})
}

func TestTopicSchemas_Validate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *config.Config)
		wantErr bool
	}{
		{name: "defaults", mutate: func(c *config.Config) {}},
		{name: "inline only", mutate: func(c *config.Config) { c.TopicsSchemas = config.NewTopicSchemas(orderSchemasJSON) }},
		{name: "file only", mutate: func(c *config.Config) { c.TopicsSchemasFile = "topics.yaml" }},
		{name: "inline and file", wantErr: true, mutate: func(c *config.Config) {
			c.TopicsSchemas = config.NewTopicSchemas(orderSchemasJSON)
			c.TopicsSchemasFile = "topics.yaml"
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := validConfig()
			tt.mutate(c)
			err := c.Validate(config.Flags{})
			if tt.wantErr {
				assert.ErrorIs(t, err, config.ErrInvalidTopicSchemas)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestTopicSchemas_Defaults(t *testing.T) {
	cfg, _ := parseTopicsConfig(t, nil, nil, "")
	assert.False(t, cfg.TopicsSchemas.IsSet())
	assert.Empty(t, cfg.TopicsSchemasFile)
}

func TestTopicSchemas_TopicCatalogBeforeLoad(t *testing.T) {
	c := &config.Config{}
	c.InitDefaults()
	c.Topics = []string{"a", "b"}
	c.TopicsSchemas = config.NewTopicSchemas(`{"a":{"validation":"bogus"}}`)

	catalog := c.TopicCatalog()
	require.NotNil(t, catalog)
	assert.False(t, catalog.HasSchemas())
	assert.Len(t, catalog.Topics(), 2)

	stored := topicschema.EmptyCatalog([]string{"x"})
	c.SetTopicCatalog(stored)
	assert.Same(t, stored, c.TopicCatalog())

	// Copies share the stored catalog.
	copied := *c
	assert.Same(t, stored, copied.TopicCatalog())
}

func TestTopicSchemas_ConfigBuiltInCodeReadsRealFiles(t *testing.T) {
	path := t.TempDir() + "/topics.json"
	require.NoError(t, os.WriteFile(path, []byte(orderSchemasJSON), 0o600))

	c := &config.Config{}
	c.InitDefaults()
	c.Topics = []string{"order.created"}
	c.TopicsSchemasFile = path
	requireOrderCreated(t, loadCatalog(t, c))
}

func TestTopicSchemas_NoIOOutsideLoad(t *testing.T) {
	osi := newTopicsOS(map[string]string{
		"TOPICS_SCHEMAS_FILE":   "topics.yaml",
		"POSTGRES_URL":          "postgres://localhost:5432/outpost",
		"RABBITMQ_SERVER_URL":   "amqp://localhost:5672",
		"AES_ENCRYPTION_SECRET": "secret",
	}, map[string]string{"topics.yaml": orderSchemasYAML})

	cfg, err := config.ParseWithOS(config.Flags{}, osi)
	require.NoError(t, err)
	require.NoError(t, cfg.Validate(config.Flags{}))
	fields := cfg.LogConfigurationSummary()

	assert.Empty(t, osi.reads, "no schema file is read before LoadTopicCatalog")
	assert.False(t, cfg.TopicCatalog().HasSchemas())

	summary := map[string]any{}
	for _, f := range fields {
		switch {
		case f.String != "":
			summary[f.Key] = f.String
		case f.Interface != nil:
			summary[f.Key] = f.Interface
		default:
			summary[f.Key] = f.Integer
		}
	}
	assert.Equal(t, "topics.yaml", summary["topics_schemas_file"])
	assert.Contains(t, summary, "topics_schemas_configured")

	_, err = cfg.LoadTopicCatalog()
	require.NoError(t, err)
	assert.Contains(t, osi.reads, "topics.yaml")
}

func indent(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n") + "\n"
}
