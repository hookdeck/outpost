package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"time"

	"github.com/hookdeck/outpost/internal/topicschema"
	"gopkg.in/yaml.v3"
)

// OpenAPI fetch limits for TOPICS_SCHEMAS_OPENAPI URLs.
const (
	openAPIFetchTimeout = 10 * time.Second
	openAPIMaxRedirects = 3
	// openAPIMaxBytes matches the topicschema.ParseOpenAPI input cap.
	openAPIMaxBytes = 4 << 20
)

// TopicSchemas holds TOPICS_SCHEMAS: topic definitions keyed by topic name,
// as raw JSON. Only LoadTopicCatalog parses it and reports its errors, so a
// service that never loads the catalog never fails on it.
//
// From the environment it is a JSON object. From YAML it is a mapping, kept
// as JSON with YAML 1.2 core types (dates and timestamps stay strings), or a
// string containing JSON. A mapping that can't be converted, for example
// because it uses merge keys, keeps the error for LoadTopicCatalog.
type TopicSchemas struct {
	raw json.RawMessage
	// err is why the YAML mapping couldn't be converted to JSON.
	err error
}

// NewTopicSchemas returns TopicSchemas holding the given JSON. Intended for
// tests and programmatic config construction.
func NewTopicSchemas(raw string) TopicSchemas {
	var t TopicSchemas
	_ = t.UnmarshalText([]byte(raw))
	return t
}

// IsSet reports whether a non-empty value was provided, including a YAML
// mapping that couldn't be converted.
func (t TopicSchemas) IsSet() bool {
	return len(t.raw) > 0 || t.err != nil
}

// Raw returns the JSON as provided. It must not be modified.
func (t TopicSchemas) Raw() json.RawMessage {
	return t.raw
}

func (t *TopicSchemas) UnmarshalText(b []byte) error {
	t.err = nil
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		t.raw = nil
		return nil
	}
	t.raw = json.RawMessage(bytes.Clone(b))
	return nil
}

func (t *TopicSchemas) UnmarshalYAML(node *yaml.Node) error {
	switch {
	case node.Tag == "!!null":
		// A bare `topics_schemas:` is unset, like an absent key.
		t.raw, t.err = nil, nil
		return nil
	case node.Kind == yaml.ScalarNode:
		var s string
		if err := node.Decode(&s); err != nil {
			return err
		}
		return t.UnmarshalText([]byte(s))
	default:
		// The error is kept, not returned: returning it would fail parsing
		// the config file for every service and command.
		t.raw, t.err = topicschema.YAMLNodeToJSON(node)
		if t.err != nil {
			t.raw, t.err = nil, fmt.Errorf("topics_schemas: %w", t.err)
		}
		return nil
	}
}

// topicSchemasError matches ErrInvalidTopicSchemas and the cause, while its
// message stays the cause's: a *topicschema.ConfigError already reads
// "invalid topic schemas: ...".
type topicSchemasError struct {
	err error
}

func (e *topicSchemasError) Error() string   { return e.err.Error() }
func (e *topicSchemasError) Unwrap() []error { return []error{ErrInvalidTopicSchemas, e.err} }

func invalidTopicSchemas(err error) error {
	var configErr *topicschema.ConfigError
	if !errors.As(err, &configErr) {
		err = fmt.Errorf("invalid topic schemas: %w", err)
	}
	return &topicSchemasError{err: err}
}

// validateTopicSchemas checks the shape of the topic schema settings. It does
// no I/O: schemas are read, fetched and compiled by LoadTopicCatalog.
func (c *Config) validateTopicSchemas() error {
	if c.TopicsSchemas.IsSet() && c.TopicsSchemasFile != "" {
		return fmt.Errorf("%w: TOPICS_SCHEMAS and TOPICS_SCHEMAS_FILE are mutually exclusive", ErrInvalidTopicSchemas)
	}
	if pin := c.TopicsSchemasOpenAPISHA256; pin != "" {
		if c.TopicsSchemasOpenAPI == "" {
			return fmt.Errorf("%w: TOPICS_SCHEMAS_OPENAPI_SHA256 is set without TOPICS_SCHEMAS_OPENAPI", ErrInvalidTopicSchemas)
		}
		if _, err := hex.DecodeString(pin); err != nil || len(pin) != sha256.Size*2 {
			return fmt.Errorf("%w: TOPICS_SCHEMAS_OPENAPI_SHA256 must be a hex SHA-256 (64 characters)", ErrInvalidTopicSchemas)
		}
	}
	if c.TopicsValidationMaxBytes <= 0 {
		return fmt.Errorf("%w: TOPICS_VALIDATION_MAX_BYTES must be greater than 0", ErrInvalidTopicSchemas)
	}
	return nil
}

// LoadTopicCatalog reads, fetches and compiles the topic schemas, then stores
// the catalog for TopicCatalog. It is the only topic schema step that does
// I/O: TOPICS_SCHEMAS_FILE is read through the OSInterface the config was
// parsed with, and a TOPICS_SCHEMAS_OPENAPI URL is fetched. Entries of
// TOPICS_SCHEMAS or TOPICS_SCHEMAS_FILE replace imported OpenAPI entries per
// topic, and OpenAPI webhooks whose topic isn't in TOPICS are skipped with a
// warning. Errors match ErrInvalidTopicSchemas.
func (c *Config) LoadTopicCatalog(ctx context.Context) (*topicschema.Catalog, error) {
	defs, err := c.loadTopicDefinitions()
	if err != nil {
		return nil, invalidTopicSchemas(err)
	}
	imported, warnings, digest, err := c.loadOpenAPIDefinitions(ctx)
	if err != nil {
		return nil, invalidTopicSchemas(err)
	}
	catalog, err := topicschema.NewCatalog(c.Topics, defs,
		topicschema.WithImported(imported),
		topicschema.WithWarnings(warnings),
		topicschema.WithMaxValidationBytes(c.TopicsValidationMaxBytes),
	)
	if err != nil {
		return nil, invalidTopicSchemas(err)
	}
	c.topicsSchemasOpenAPIDigest = digest
	c.SetTopicCatalog(catalog)
	return catalog, nil
}

// SetTopicCatalog stores the catalog returned by TopicCatalog.
func (c *Config) SetTopicCatalog(catalog *topicschema.Catalog) {
	c.topicCatalog = catalog
}

// TopicCatalog returns the catalog stored by LoadTopicCatalog, or a catalog
// of schema-less TOPICS when none was loaded.
func (c *Config) TopicCatalog() *topicschema.Catalog {
	if c.topicCatalog != nil {
		return c.topicCatalog
	}
	return topicschema.EmptyCatalog(c.Topics)
}

// TopicsSchemasOpenAPIDigest returns the hex SHA-256 of the OpenAPI document
// loaded by LoadTopicCatalog, or "" when none was loaded.
func (c *Config) TopicsSchemasOpenAPIDigest() string {
	return c.topicsSchemasOpenAPIDigest
}

// osOrDefault returns the OSInterface the config was parsed with, or the
// real OS for a config built in code.
func (c *Config) osOrDefault() OSInterface {
	if c.osInterface != nil {
		return c.osInterface
	}
	return defaultOS
}

// loadTopicDefinitions parses TOPICS_SCHEMAS or TOPICS_SCHEMAS_FILE. A file
// ending in .json is JSON; any other is YAML.
func (c *Config) loadTopicDefinitions() (topicschema.Definitions, error) {
	if c.TopicsSchemas.err != nil {
		return nil, c.TopicsSchemas.err
	}
	if c.TopicsSchemas.IsSet() {
		return topicschema.ParseDefinitionsJSON(c.TopicsSchemas.Raw())
	}
	if c.TopicsSchemasFile == "" {
		return nil, nil
	}
	data, err := c.osOrDefault().ReadFile(c.TopicsSchemasFile)
	if err != nil {
		return nil, fmt.Errorf("reading TOPICS_SCHEMAS_FILE: %w", err)
	}
	if strings.EqualFold(filepath.Ext(c.TopicsSchemasFile), ".json") {
		return topicschema.ParseDefinitionsJSON(data)
	}
	return topicschema.ParseDefinitionsYAML(data)
}

// loadOpenAPIDefinitions imports TOPICS_SCHEMAS_OPENAPI and returns the
// definitions of the topics in TOPICS, warnings about the webhooks skipped,
// and the hex SHA-256 of the document.
func (c *Config) loadOpenAPIDefinitions(ctx context.Context) (topicschema.Definitions, []string, string, error) {
	source := c.TopicsSchemasOpenAPI
	if source == "" {
		return nil, nil, "", nil
	}
	var data []byte
	var err error
	if isURL(source) {
		data, err = fetchOpenAPI(ctx, source)
	} else {
		data, err = c.osOrDefault().ReadFile(source)
		if err == nil && len(data) > openAPIMaxBytes {
			err = fmt.Errorf("the OpenAPI document is larger than the %d MiB limit", openAPIMaxBytes>>20)
		}
	}
	if err != nil {
		return nil, nil, "", fmt.Errorf("loading TOPICS_SCHEMAS_OPENAPI %s: %w", maskOpenAPISource(source), err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	if pin := c.TopicsSchemasOpenAPISHA256; pin != "" && !strings.EqualFold(pin, digest) {
		return nil, nil, "", fmt.Errorf("TOPICS_SCHEMAS_OPENAPI %s has SHA-256 %s, but TOPICS_SCHEMAS_OPENAPI_SHA256 is %s",
			maskOpenAPISource(source), digest, strings.ToLower(pin))
	}
	// An empty TOPICS allows any topic: everything is imported, and
	// NewCatalog rejects definitions it can't check.
	var opts []topicschema.OpenAPIOption
	if len(c.Topics) > 0 {
		opts = append(opts, topicschema.OpenAPITopics(c.Topics))
	}
	defs, warnings, err := topicschema.ParseOpenAPI(data, opts...)
	if err != nil {
		return nil, nil, "", err
	}
	return defs, warnings, digest, nil
}

// isURL reports whether source has a URL scheme rather than being a file
// path. Only http and https are fetched; other schemes fail the fetch.
func isURL(source string) bool {
	scheme, _, ok := strings.Cut(source, "://")
	return ok && scheme != "" && !strings.ContainsAny(scheme, `/\`)
}

// fetchOpenAPI downloads an OpenAPI document: status 200 within the timeout,
// at most openAPIMaxRedirects redirects never from https to http, and at most
// openAPIMaxBytes, which fails rather than truncates. Errors never include
// the URL, which may carry credentials.
func fetchOpenAPI(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("invalid URL")
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported URL scheme %q: use a file path or an http(s) URL", u.Scheme)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, errors.New("invalid URL")
	}
	client := &http.Client{Timeout: openAPIFetchTimeout, CheckRedirect: checkOpenAPIRedirect}
	resp, err := client.Do(req)
	if err != nil {
		// *url.Error repeats the URL; keep only the cause.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d, want 200", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, openAPIMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("reading the response: %w", err)
	}
	if len(data) > openAPIMaxBytes {
		return nil, fmt.Errorf("the OpenAPI document is larger than the %d MiB limit", openAPIMaxBytes>>20)
	}
	return data, nil
}

func checkOpenAPIRedirect(req *http.Request, via []*http.Request) error {
	if len(via) > openAPIMaxRedirects {
		return fmt.Errorf("stopped after %d redirects", openAPIMaxRedirects)
	}
	if via[len(via)-1].URL.Scheme == "https" && req.URL.Scheme != "https" {
		return errors.New("refused a redirect from https to http")
	}
	if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
		return fmt.Errorf("refused a redirect to scheme %q", req.URL.Scheme)
	}
	return nil
}

// maskOpenAPISource hides the userinfo and query string of an OpenAPI URL,
// which may carry credentials. File paths are returned as is.
func maskOpenAPISource(source string) string {
	if !isURL(source) {
		return source
	}
	u, err := url.Parse(source)
	if err != nil {
		return "<invalid URL>"
	}
	masked := url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path, RawPath: u.RawPath}
	out := masked.String()
	if u.User != nil {
		out = strings.Replace(out, "://", "://***@", 1)
	}
	if u.RawQuery != "" || u.ForceQuery {
		out += "?***"
	}
	return out
}
