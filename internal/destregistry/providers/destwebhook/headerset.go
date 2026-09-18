package destwebhook

import (
	"bytes"
	"fmt"
	"net/http"
	"text/template"
)

// signatureScheme is how a signature is computed: templates, encoding,
// algorithm and key derivation. Built once from config and shared by every
// publisher, since parsed templates are safe for parallel execution.
type signatureScheme struct {
	signatureFormatter SignatureFormatter
	headerFormatter    HeaderFormatter
	encoder            SignatureEncoder
	algorithm          SigningAlgorithm
	secretEncoding     string
	secretPrefix       string
}

type signatureSchemeConfig struct {
	ContentTemplate string
	HeaderTemplate  string
	Encoding        string
	Algorithm       string
	SecretEncoding  string
	SecretPrefix    string
}

func newSignatureScheme(cfg signatureSchemeConfig) (*signatureScheme, error) {
	if cfg.Encoding == "" {
		return nil, fmt.Errorf("signature encoding is required")
	}
	if cfg.Algorithm == "" {
		return nil, fmt.Errorf("signature algorithm is required")
	}
	if cfg.ContentTemplate == "" {
		return nil, fmt.Errorf("signature content template is required")
	}
	if cfg.HeaderTemplate == "" {
		return nil, fmt.Errorf("signature header template is required")
	}
	if err := validateSecretEncoding(cfg.SecretEncoding); err != nil {
		return nil, err
	}

	s := &signatureScheme{
		encoder:        GetEncoder(cfg.Encoding),
		algorithm:      GetAlgorithm(cfg.Algorithm),
		secretEncoding: cfg.SecretEncoding,
		secretPrefix:   cfg.SecretPrefix,
	}
	var err error
	if s.signatureFormatter, err = NewSignatureFormatter(cfg.ContentTemplate); err != nil {
		return nil, err
	}
	if s.headerFormatter, err = NewHeaderFormatter(cfg.HeaderTemplate); err != nil {
		return nil, err
	}
	// Dry-run render both, so a template that parses but references a field
	// its payload type doesn't have fails construction even when the provider
	// is built outside config validation.
	if err := dryRunFormatters(s.signatureFormatter, s.headerFormatter, cfg.ContentTemplate, cfg.HeaderTemplate); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *signatureScheme) decodeSecret(secret string) (string, error) {
	return decodeSecretKey(secret, s.secretEncoding, s.secretPrefix)
}

// manager builds the per-destination signer, deriving keys from the stored
// secrets according to the scheme's secret encoding.
func (s *signatureScheme) manager(secrets []WebhookSecret) (*SignatureManager, error) {
	keys := make([]WebhookSecret, len(secrets))
	for i, secret := range secrets {
		key, err := s.decodeSecret(secret.Key)
		if err != nil {
			return nil, err
		}
		keys[i] = secret
		keys[i].Key = key
	}
	return NewSignatureManager(
		keys,
		WithSignatureFormatter(s.signatureFormatter),
		WithHeaderFormatter(s.headerFormatter),
		WithEncoder(s.encoder),
		WithAlgorithm(s.algorithm),
	), nil
}

type headerTemplate struct {
	name string
	tmpl *template.Template
}

// headerSet is everything one signature scheme puts on a request: templated
// headers rendered from the signature payload, then the signature header.
type headerSet struct {
	headers       []headerTemplate
	signatureName string // empty disables the signature header
}

func (h headerTemplate) render(payload SignaturePayload) (string, error) {
	var buf bytes.Buffer
	if err := h.tmpl.Execute(&buf, payload); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// apply writes the set's headers onto req. Later sets overwrite earlier ones
// on a name conflict, so callers order them by precedence.
func (h *headerSet) apply(req *http.Request, sm *SignatureManager, payload SignaturePayload) error {
	for _, header := range h.headers {
		value, err := header.render(payload)
		if err != nil {
			return fmt.Errorf("header template for %q failed: %w", header.name, err)
		}
		req.Header.Set(header.name, value)
	}
	if h.signatureName == "" || sm == nil {
		return nil
	}
	value, err := sm.GenerateSignatureHeader(payload)
	if err != nil {
		return err
	}
	if value != "" {
		req.Header.Set(h.signatureName, value)
	}
	return nil
}
