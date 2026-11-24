package destwebhook

import (
	"fmt"
	"strings"
)

// CompatSignatureConfig describes a second signature sent alongside the primary
// one, so receivers verifying a previous scheme keep working while they migrate.
// It takes the same options as the primary signature.
type CompatSignatureConfig struct {
	SignatureHeaderName      string
	SignatureContentTemplate string
	SignatureHeaderTemplate  string
	SignatureEncoding        string
	SignatureAlgorithm       string
	SecretEncoding           string
	SecretPrefix             string
	// Headers are companion headers the scheme needs (ID, timestamp), as header
	// name -> template rendered against SignaturePayload.
	Headers map[string]string
}

func WithCompatSignature(cfg *CompatSignatureConfig) Option {
	return func(w *WebhookDestination) {
		w.compatConfig = cfg
	}
}

type compatSignature struct {
	headerSet
	scheme *signatureScheme
}

func newCompatSignature(cfg *CompatSignatureConfig) (*compatSignature, error) {
	name := strings.TrimSpace(cfg.SignatureHeaderName)
	if name == "" {
		return nil, fmt.Errorf("compat signature header name is required")
	}
	if err := validateHeaderName(name, "compat signature"); err != nil {
		return nil, err
	}
	for header := range cfg.Headers {
		if strings.EqualFold(header, name) {
			return nil, fmt.Errorf("compat header %q collides with the compat signature header", header)
		}
	}
	scheme, err := newSignatureScheme(signatureSchemeConfig{
		ContentTemplate: cfg.SignatureContentTemplate,
		HeaderTemplate:  cfg.SignatureHeaderTemplate,
		Encoding:        cfg.SignatureEncoding,
		Algorithm:       cfg.SignatureAlgorithm,
		SecretEncoding:  cfg.SecretEncoding,
		SecretPrefix:    cfg.SecretPrefix,
	})
	if err != nil {
		return nil, fmt.Errorf("compat signature: %w", err)
	}
	headers, err := parseHeaderTemplates(cfg.Headers, "compat")
	if err != nil {
		return nil, err
	}
	return &compatSignature{
		headerSet: headerSet{headers: headers, signatureName: name},
		scheme:    scheme,
	}, nil
}
