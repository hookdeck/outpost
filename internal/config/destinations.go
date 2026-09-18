package config

import (
	"fmt"
	"strings"

	destregistrydefault "github.com/hookdeck/outpost/internal/destregistry/providers"
	"github.com/hookdeck/outpost/internal/destregistry/providers/destwebhook"
	"github.com/hookdeck/outpost/internal/version"
)

// DestinationsConfig is the main configuration for all destination types
type DestinationsConfig struct {
	MetadataPath                string                      `yaml:"metadata_path" env:"DESTINATIONS_METADATA_PATH" desc:"Path to the directory containing custom destination type definitions." required:"N"`
	IncludeMillisecondTimestamp bool                        `yaml:"include_millisecond_timestamp" env:"DESTINATIONS_INCLUDE_MILLISECOND_TIMESTAMP" desc:"If true, includes a 'timestamp-ms' field with millisecond precision in destination metadata. Useful for load testing and debugging." required:"N"`
	Webhook                     DestinationWebhookConfig    `yaml:"webhook" desc:"Configuration specific to webhook destinations."`
	AWSKinesis                  DestinationAWSKinesisConfig `yaml:"aws_kinesis" desc:"Configuration specific to AWS Kinesis destinations."`
}

func (c *DestinationsConfig) ToConfig(cfg *Config) destregistrydefault.RegisterDefaultDestinationOptions {
	userAgent := cfg.HTTPUserAgent
	if userAgent == "" {
		userAgent = fmt.Sprintf("Outpost/%s", version.Version())
	}

	return destregistrydefault.RegisterDefaultDestinationOptions{
		UserAgent:                   userAgent,
		IncludeMillisecondTimestamp: c.IncludeMillisecondTimestamp,
		Webhook:                     c.Webhook.toConfig(),
		AWSKinesis:                  c.AWSKinesis.toConfig(),
		DeliveryMaxConcurrency:      cfg.DeliveryMaxConcurrency,
	}
}

// DefaultWebhookMaxResponseBodyBytes is the default cap on the destination
// response body stored on a delivery attempt: 128 KiB.
//
// The attempt log (event + attempt, response body included) is published to the
// configured message queue, so it must fit that queue's per-message limit. The
// strictest limits among supported queues are 256 KiB (SQS default queue
// attribute) and 256 KB (Azure Service Bus standard tier); Pub/Sub (10 MB) and
// RabbitMQ (16 MiB default) allow far more. We considered deriving the default
// per queue, but for three of the four the real limit is deployment-side config
// we can't see (SQS queue attribute, Azure tier, RabbitMQ server setting), so a
// single conservative value keeps it simple: half the 256 KiB floor, leaving the
// other half for the event payload and envelope. Webhook responses are typically
// small acknowledgments, so 128 KiB should be far more than any well-behaved
// destination returns.
const DefaultWebhookMaxResponseBodyBytes = 131072

// Webhook configuration
type DestinationWebhookConfig struct {
	// ProxyURL may contain authentication credentials (e.g., http://user:pass@proxy:8080)
	// and should be treated as sensitive.
	// TODO: Implement sensitive value handling - https://github.com/hookdeck/outpost/issues/480
	Mode         string `yaml:"mode" env:"DESTINATIONS_WEBHOOK_MODE" desc:"Webhook mode: 'default' or 'standard'. 'standard' changes the defaults of the DESTINATIONS_WEBHOOK_* options to the Standard Webhooks specification (webhook- prefix, webhook-id and unix webhook-timestamp headers, base64 'v1,' signatures over id.timestamp.body, whsec_ base64 secrets). Any option set explicitly still applies. Defaults to 'default'." required:"N"`
	ProxyURL     string `yaml:"proxy_url" env:"DESTINATIONS_WEBHOOK_PROXY_URL" desc:"Forward proxy for outgoing webhook requests (HTTP or HTTPS, basic auth supported). Multiple whitespace-separated URLs are tunneled in order." required:"N"`
	HeaderPrefix string `yaml:"header_prefix" env:"DESTINATIONS_WEBHOOK_HEADER_PREFIX" desc:"Prefix for metadata headers added to webhook requests. Defaults to 'x-outpost-' in 'default' mode and 'webhook-' in 'standard' mode. Set to whitespace (e.g. ' ') to disable the prefix entirely." required:"N"`

	// Header name configs. Each is three-state: unset uses the default
	// '<prefix>' + key, an explicit value pins that exact header name, and an
	// empty string disables the header entirely.
	EventIDHeaderName   OptionalString `yaml:"event_id_header_name" env:"DESTINATIONS_WEBHOOK_EVENT_ID_HEADER_NAME" desc:"Complete name of the event ID header. Unset uses the default '<prefix>event-id'; an explicit value pins that exact name; an empty string disables the header." required:"N"`
	SignatureHeaderName OptionalString `yaml:"signature_header_name" env:"DESTINATIONS_WEBHOOK_SIGNATURE_HEADER_NAME" desc:"Complete name of the signature header. Unset uses the default '<prefix>signature'; an explicit value pins that exact name; an empty string disables the header." required:"N"`
	TimestampHeaderName OptionalString `yaml:"timestamp_header_name" env:"DESTINATIONS_WEBHOOK_TIMESTAMP_HEADER_NAME" desc:"Complete name of the timestamp header. Unset uses the default '<prefix>timestamp'; an explicit value pins that exact name; an empty string disables the header." required:"N"`
	TopicHeaderName     OptionalString `yaml:"topic_header_name" env:"DESTINATIONS_WEBHOOK_TOPIC_HEADER_NAME" desc:"Complete name of the topic header. Unset uses the default '<prefix>topic'; an explicit value pins that exact name; an empty string disables the header." required:"N"`

	// Deprecated: replaced by the *_HEADER_NAME configs above. Setting one of
	// these to true still disables the corresponding header (an empty
	// *_HEADER_NAME is the replacement) but logs a deprecation warning. A new
	// *_HEADER_NAME config always takes precedence over the matching flag.
	DisableDefaultEventIDHeader   bool `yaml:"disable_default_event_id_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_EVENT_ID_HEADER" desc:"Deprecated: set DESTINATIONS_WEBHOOK_EVENT_ID_HEADER_NAME to an empty string to disable the event ID header instead." required:"N"`
	DisableDefaultSignatureHeader bool `yaml:"disable_default_signature_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_SIGNATURE_HEADER" desc:"Deprecated: set DESTINATIONS_WEBHOOK_SIGNATURE_HEADER_NAME to an empty string to disable the signature header instead." required:"N"`
	DisableDefaultTimestampHeader bool `yaml:"disable_default_timestamp_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_TIMESTAMP_HEADER" desc:"Deprecated: set DESTINATIONS_WEBHOOK_TIMESTAMP_HEADER_NAME to an empty string to disable the timestamp header instead." required:"N"`
	DisableDefaultTopicHeader     bool `yaml:"disable_default_topic_header" env:"DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_TOPIC_HEADER" desc:"Deprecated: set DESTINATIONS_WEBHOOK_TOPIC_HEADER_NAME to an empty string to disable the topic header instead." required:"N"`

	TimestampFormat string `yaml:"timestamp_format" env:"DESTINATIONS_WEBHOOK_TIMESTAMP_FORMAT" desc:"Format of the timestamp header: 'rfc3339' or 'unix' (seconds since the epoch). Defaults to 'rfc3339' in 'default' mode and 'unix' in 'standard' mode." required:"N"`

	SignatureContentTemplate string `yaml:"signature_content_template" env:"DESTINATIONS_WEBHOOK_SIGNATURE_CONTENT_TEMPLATE" desc:"Go template for constructing the content to be signed for webhook requests." required:"N"`
	SignatureHeaderTemplate  string `yaml:"signature_header_template" env:"DESTINATIONS_WEBHOOK_SIGNATURE_HEADER_TEMPLATE" desc:"Go template for the value of the signature header." required:"N"`
	SignatureEncoding        string `yaml:"signature_encoding" env:"DESTINATIONS_WEBHOOK_SIGNATURE_ENCODING" desc:"Encoding for the signature (e.g., 'hex', 'base64')." required:"N"`
	SignatureAlgorithm       string `yaml:"signature_algorithm" env:"DESTINATIONS_WEBHOOK_SIGNATURE_ALGORITHM" desc:"Algorithm used for signing webhook requests (e.g., 'hmac-sha256')." required:"N"`
	SignatureSecretEncoding  string `yaml:"signature_secret_encoding" env:"DESTINATIONS_WEBHOOK_SIGNATURE_SECRET_ENCODING" desc:"How the HMAC key is derived from the destination secret: 'raw' uses the secret string as-is, 'base64' and 'hex' decode it. Defaults to 'raw'. DESTINATIONS_WEBHOOK_SIGNING_SECRET_TEMPLATE must generate secrets this encoding can decode." required:"N"`
	SignatureSecretPrefix    string `yaml:"signature_secret_prefix" env:"DESTINATIONS_WEBHOOK_SIGNATURE_SECRET_PREFIX" desc:"Prefix stripped from the destination secret before decoding (e.g., 'whsec_'). Ignored when DESTINATIONS_WEBHOOK_SIGNATURE_SECRET_ENCODING is 'raw'." required:"N"`
	SigningSecretTemplate    string `yaml:"signing_secret_template" env:"DESTINATIONS_WEBHOOK_SIGNING_SECRET_TEMPLATE" desc:"Go template for generating webhook signing secrets. Available variables: {{.RandomHex}} (64-char hex), {{.RandomBase64}} (base64-encoded), {{.RandomAlphanumeric}} (32-char alphanumeric). Defaults to 'whsec_{{.RandomHex}}'." required:"N"`
	MaxResponseBodyBytes     int    `yaml:"max_response_body_bytes" env:"DESTINATIONS_WEBHOOK_MAX_RESPONSE_BODY_BYTES" desc:"Maximum size in bytes of a destination's response body stored on the delivery attempt. Responses larger than this are replaced with a placeholder so the attempt log stays under the event queue's per-message size limit (oversized log messages fail to publish and retry indefinitely). Default: 131072 (128 KiB). Set to 0 to disable the cap." required:"N"`

	Compat DestinationWebhookCompatConfig `yaml:"compat" desc:"A second signature sent alongside the primary one, so receivers verifying an older scheme keep working while they migrate."`
}

// DestinationWebhookCompatConfig mirrors the primary signature options. It is
// enabled by setting SignatureHeaderName; the other signature options then
// fall back to the same defaults as the primary signature.
type DestinationWebhookCompatConfig struct {
	SignatureHeaderName      string          `yaml:"signature_header_name" env:"DESTINATIONS_WEBHOOK_COMPAT_SIGNATURE_HEADER_NAME" desc:"Complete name of the compat signature header. Setting it enables the compat signature." required:"N"`
	SignatureContentTemplate string          `yaml:"signature_content_template" env:"DESTINATIONS_WEBHOOK_COMPAT_SIGNATURE_CONTENT_TEMPLATE" desc:"Go template for the content signed by the compat signature. Same variables as DESTINATIONS_WEBHOOK_SIGNATURE_CONTENT_TEMPLATE. Defaults to '{{.Body}}'." required:"N"`
	SignatureHeaderTemplate  string          `yaml:"signature_header_template" env:"DESTINATIONS_WEBHOOK_COMPAT_SIGNATURE_HEADER_TEMPLATE" desc:"Go template for the value of the compat signature header. Same variables as DESTINATIONS_WEBHOOK_SIGNATURE_HEADER_TEMPLATE. Defaults to 'v0={{.Signatures | join \",\"}}'." required:"N"`
	SignatureEncoding        string          `yaml:"signature_encoding" env:"DESTINATIONS_WEBHOOK_COMPAT_SIGNATURE_ENCODING" desc:"Encoding for the compat signature ('hex' or 'base64'). Defaults to 'hex'." required:"N"`
	SignatureAlgorithm       string          `yaml:"signature_algorithm" env:"DESTINATIONS_WEBHOOK_COMPAT_SIGNATURE_ALGORITHM" desc:"Algorithm for the compat signature (e.g., 'hmac-sha256'). Defaults to 'hmac-sha256'." required:"N"`
	SignatureSecretEncoding  string          `yaml:"signature_secret_encoding" env:"DESTINATIONS_WEBHOOK_COMPAT_SIGNATURE_SECRET_ENCODING" desc:"How the compat signature derives its HMAC key from the destination secret: 'raw', 'base64' or 'hex'. Defaults to 'raw'. A destination whose secret can't be decoded doesn't receive the compat signature." required:"N"`
	SignatureSecretPrefix    string          `yaml:"signature_secret_prefix" env:"DESTINATIONS_WEBHOOK_COMPAT_SIGNATURE_SECRET_PREFIX" desc:"Prefix stripped from the destination secret before decoding for the compat signature (e.g., 'whsec_')." required:"N"`
	Headers                  HeaderTemplates `yaml:"headers" env:"DESTINATIONS_WEBHOOK_COMPAT_HEADERS" desc:"Additional headers sent with the compat signature, as comma-separated 'name=template' pairs. Available variables: {{.EventID}}, {{.Topic}}, {{.Timestamp}}. Use '\\,' for a literal comma in a template. Example: 'webhook-id={{.EventID}},webhook-timestamp={{.Timestamp.Unix}}'." required:"N"`
}

// toProviderConfig returns nil when the compat signature isn't configured.
func (c *DestinationWebhookCompatConfig) toProviderConfig() *destwebhook.CompatSignatureConfig {
	if strings.TrimSpace(c.SignatureHeaderName) == "" {
		return nil
	}

	cfg := &destwebhook.CompatSignatureConfig{
		SignatureHeaderName:      c.SignatureHeaderName,
		SignatureContentTemplate: c.SignatureContentTemplate,
		SignatureHeaderTemplate:  c.SignatureHeaderTemplate,
		SignatureEncoding:        c.SignatureEncoding,
		SignatureAlgorithm:       c.SignatureAlgorithm,
		SecretEncoding:           c.SignatureSecretEncoding,
		SecretPrefix:             c.SignatureSecretPrefix,
	}
	if cfg.SignatureContentTemplate == "" {
		cfg.SignatureContentTemplate = destwebhook.DefaultSignatureContentTmpl
	}
	if cfg.SignatureHeaderTemplate == "" {
		cfg.SignatureHeaderTemplate = destwebhook.DefaultSignatureHeaderTmpl
	}
	if cfg.SignatureEncoding == "" {
		cfg.SignatureEncoding = destwebhook.DefaultEncoding
	}
	if cfg.SignatureAlgorithm == "" {
		cfg.SignatureAlgorithm = destwebhook.DefaultAlgorithm
	}

	if len(c.Headers) > 0 {
		cfg.Headers = map[string]string(c.Headers)
	}

	return cfg
}

// toConfig converts WebhookConfig to the provider config - private since it's only used internally
// Config guarantees all required values are set via setDefaults()
func (c *DestinationWebhookConfig) toConfig() *destregistrydefault.DestWebhookConfig {
	// HeaderPrefix: config provides mode-specific defaults
	// - user sets "" → config applies default based on mode ("x-outpost-" or "webhook-")
	// - user sets " " → whitespace passes through → provider trims to "" (disabled)
	// - user sets explicit value → passes through as-is
	headerPrefix := c.HeaderPrefix
	if headerPrefix == "" {
		// Apply mode-specific default only when truly empty (not whitespace)
		if c.Mode == "standard" {
			headerPrefix = destwebhook.StandardHeaderPrefix
		} else {
			headerPrefix = destwebhook.DefaultHeaderPrefix
		}
	}

	cfg := &destregistrydefault.DestWebhookConfig{
		Compat:                   c.Compat.toProviderConfig(),
		SignatureSecretEncoding:  c.SignatureSecretEncoding,
		SignatureSecretPrefix:    c.SignatureSecretPrefix,
		Mode:                     c.Mode,
		ProxyURL:                 c.ProxyURL,
		HeaderPrefix:             headerPrefix,
		EventIDHeader:            resolveWebhookHeaderName(c.EventIDHeaderName, c.DisableDefaultEventIDHeader),
		SignatureHeader:          resolveWebhookHeaderName(c.SignatureHeaderName, c.DisableDefaultSignatureHeader),
		TimestampHeader:          resolveWebhookHeaderName(c.TimestampHeaderName, c.DisableDefaultTimestampHeader),
		TopicHeader:              resolveWebhookHeaderName(c.TopicHeaderName, c.DisableDefaultTopicHeader),
		TimestampFormat:          c.TimestampFormat,
		SignatureContentTemplate: c.SignatureContentTemplate,
		SignatureHeaderTemplate:  c.SignatureHeaderTemplate,
		SignatureEncoding:        c.SignatureEncoding,
		SignatureAlgorithm:       c.SignatureAlgorithm,
		SigningSecretTemplate:    c.SigningSecretTemplate,
		MaxResponseBodyBytes:     c.MaxResponseBodyBytes,
	}
	if c.Mode == "standard" {
		applyStandardWebhooksDefaults(cfg)
	}
	return cfg
}

// applyStandardWebhooksDefaults turns "standard" mode into the Standard
// Webhooks values of the webhook options. Defaults are set before the config is
// parsed, so an option still at its default-mode default is taken as unset and
// replaced; an operator value that differs is kept.
func applyStandardWebhooksDefaults(cfg *destregistrydefault.DestWebhookConfig) {
	for _, d := range []struct {
		field        *string
		defaultMode  string
		standardMode string
	}{
		{&cfg.SignatureContentTemplate, destwebhook.DefaultSignatureContentTmpl, destwebhook.StandardSignatureContentTmpl},
		{&cfg.SignatureHeaderTemplate, destwebhook.DefaultSignatureHeaderTmpl, destwebhook.StandardSignatureHeaderTmpl},
		{&cfg.SignatureEncoding, destwebhook.DefaultEncoding, destwebhook.StandardEncoding},
		{&cfg.SigningSecretTemplate, destwebhook.DefaultSigningSecretTmpl, destwebhook.StandardSigningSecretTmpl},
		{&cfg.SignatureSecretEncoding, "", destwebhook.StandardSecretEncoding},
		{&cfg.SignatureSecretPrefix, "", destwebhook.StandardSecretPrefix},
		{&cfg.TimestampFormat, "", destwebhook.StandardTimestampFormat},
	} {
		if *d.field == d.defaultMode {
			*d.field = d.standardMode
		}
	}
	// Standard Webhooks names the event ID header "<prefix>id".
	if cfg.EventIDHeader == (destregistrydefault.WebhookHeaderConfig{}) {
		cfg.EventIDHeader.Name = strings.TrimSpace(cfg.HeaderPrefix) + destwebhook.StandardEventIDHeaderKey
	}
}

// resolveWebhookHeaderName applies the three-state rule for a webhook header
// name, folding in the deprecated DISABLE_* flag for backward compatibility:
//   - name set to a value  -> {Name: value}                 (pin exact name)
//   - name set to "" or whitespace-only -> {Disabled: true} (disable)
//   - name unset, flag true -> {Disabled: true}             (deprecated disable)
//   - name unset, flag false -> {}                          (provider builds <prefix>+key)
//
// A set name always wins over the deprecated flag.
func resolveWebhookHeaderName(name OptionalString, deprecatedDisable bool) destregistrydefault.WebhookHeaderConfig {
	if value, set := name.Get(); set {
		if strings.TrimSpace(value) == "" {
			return destregistrydefault.WebhookHeaderConfig{Disabled: true}
		}
		return destregistrydefault.WebhookHeaderConfig{Name: value}
	}
	if deprecatedDisable {
		return destregistrydefault.WebhookHeaderConfig{Disabled: true}
	}
	return destregistrydefault.WebhookHeaderConfig{}
}

// deprecationWarnings returns a message for each deprecated DISABLE_* flag that
// is actively set to true (the case that changes behavior). A set *_HEADER_NAME
// config still takes precedence over the flag, but the warning is emitted
// regardless so operators migrate off the deprecated var.
func (c *DestinationWebhookConfig) deprecationWarnings() []string {
	var warnings []string
	for _, d := range []struct {
		enabled bool
		oldEnv  string
		newEnv  string
	}{
		{c.DisableDefaultEventIDHeader, "DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_EVENT_ID_HEADER", "DESTINATIONS_WEBHOOK_EVENT_ID_HEADER_NAME"},
		{c.DisableDefaultSignatureHeader, "DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_SIGNATURE_HEADER", "DESTINATIONS_WEBHOOK_SIGNATURE_HEADER_NAME"},
		{c.DisableDefaultTimestampHeader, "DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_TIMESTAMP_HEADER", "DESTINATIONS_WEBHOOK_TIMESTAMP_HEADER_NAME"},
		{c.DisableDefaultTopicHeader, "DESTINATIONS_WEBHOOK_DISABLE_DEFAULT_TOPIC_HEADER", "DESTINATIONS_WEBHOOK_TOPIC_HEADER_NAME"},
	} {
		if d.enabled {
			warnings = append(warnings, fmt.Sprintf(
				"%s is deprecated and will be removed in a future version. Set %s to an empty string to disable the header instead.",
				d.oldEnv, d.newEnv,
			))
		}
	}
	return warnings
}

// AWS Kinesis configuration
type DestinationAWSKinesisConfig struct {
	MetadataInPayload bool `yaml:"metadata_in_payload" env:"DESTINATIONS_AWS_KINESIS_METADATA_IN_PAYLOAD" desc:"If true, includes Outpost metadata (event ID, topic, etc.) within the Kinesis record payload." required:"N"`
}

// toConfig converts AWSKinesisConfig to the provider config
func (c *DestinationAWSKinesisConfig) toConfig() *destregistrydefault.DestAWSKinesisConfig {
	return &destregistrydefault.DestAWSKinesisConfig{
		MetadataInPayload: c.MetadataInPayload,
	}
}
