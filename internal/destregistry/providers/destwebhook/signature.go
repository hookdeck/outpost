package destwebhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
	"sort"
	"text/template"
	"time"

	"github.com/Masterminds/sprig/v3"
)

type SignaturePayload struct {
	EventID   string
	Topic     string
	Timestamp time.Time
	Body      string
}

type HeaderPayload struct {
	EventID    string
	Topic      string
	Timestamp  time.Time
	Signatures []string
}

type SigningAlgorithm interface {
	Sign(key string, content string, encoder SignatureEncoder) string
	Verify(key string, content string, signature string, encoder SignatureEncoder) bool
	Name() string
}

type SignatureFormatter interface {
	Format(content SignaturePayload) (string, error)
}

type HeaderFormatter interface {
	Format(content HeaderPayload) (string, error)
}

type SignatureEncoder interface {
	Encode([]byte) string
}

type HexEncoder struct{}

func (e HexEncoder) Encode(b []byte) string {
	return hex.EncodeToString(b)
}

type Base64Encoder struct{}

func (e Base64Encoder) Encode(b []byte) string {
	return base64.StdEncoding.EncodeToString(b)
}

type SignatureFormatterImpl struct {
	template *template.Template
}

func NewSignatureFormatter(templateStr string) (*SignatureFormatterImpl, error) {
	if templateStr == "" {
		return nil, fmt.Errorf("signature content template is required")
	}

	tmpl := template.New("signature").Funcs(sprig.TxtFuncMap())

	parsed, err := tmpl.Parse(templateStr)
	if err != nil {
		return nil, fmt.Errorf("invalid signature content template %q: %w", templateStr, err)
	}

	return &SignatureFormatterImpl{template: parsed}, nil
}

// Format renders the content template. Parsing only validates syntax; field
// references are resolved against the payload at execution, so a template that
// constructs fine can still fail here. The error is returned so the caller can
// fail the delivery instead of taking the process down.
func (f *SignatureFormatterImpl) Format(content SignaturePayload) (string, error) {
	var buf bytes.Buffer
	if err := f.template.Execute(&buf, content); err != nil {
		return "", fmt.Errorf("signature content template execution failed: %w", err)
	}
	return buf.String(), nil
}

type HeaderFormatterImpl struct {
	template *template.Template
}

func NewHeaderFormatter(templateStr string) (*HeaderFormatterImpl, error) {
	if templateStr == "" {
		return nil, fmt.Errorf("signature header template is required")
	}

	tmpl := template.New("header").Funcs(sprig.TxtFuncMap())

	parsed, err := tmpl.Parse(templateStr)
	if err != nil {
		return nil, fmt.Errorf("invalid signature header template %q: %w", templateStr, err)
	}

	return &HeaderFormatterImpl{template: parsed}, nil
}

// Format renders the header template. See SignatureFormatterImpl.Format for why
// execution errors are returned rather than fatal.
func (f *HeaderFormatterImpl) Format(content HeaderPayload) (string, error) {
	var buf bytes.Buffer
	if err := f.template.Execute(&buf, content); err != nil {
		return "", fmt.Errorf("signature header template execution failed: %w", err)
	}
	return buf.String(), nil
}

type HmacAlgo struct {
	name string
	hash func() hash.Hash
}

func NewHmacSHA256() *HmacAlgo {
	return &HmacAlgo{
		name: "hmac-sha256",
		hash: sha256.New,
	}
}

func NewHmacSHA1() *HmacAlgo {
	return &HmacAlgo{
		name: "hmac-sha1",
		hash: sha1.New,
	}
}

func NewHmacMD5() *HmacAlgo {
	return &HmacAlgo{
		name: "hmac-md5",
		hash: md5.New,
	}
}

func (h *HmacAlgo) Name() string {
	return h.name
}

func (h *HmacAlgo) Sign(key string, content string, encoder SignatureEncoder) string {
	mac := hmac.New(h.hash, []byte(key))
	mac.Write([]byte(content))
	return encoder.Encode(mac.Sum(nil))
}

func (h *HmacAlgo) Verify(key string, content string, signature string, encoder SignatureEncoder) bool {
	expectedSignature := h.Sign(key, content, encoder)
	return hmac.Equal([]byte(signature), []byte(expectedSignature))
}

type SignatureManager struct {
	secrets         []WebhookSecret
	algorithm       SigningAlgorithm
	encoder         SignatureEncoder
	sigFormatter    SignatureFormatter
	headerFormatter HeaderFormatter
}

type SignatureManagerOption func(*SignatureManager)

func WithAlgorithm(algo SigningAlgorithm) SignatureManagerOption {
	return func(sm *SignatureManager) {
		sm.algorithm = algo
	}
}

func WithEncoder(encoder SignatureEncoder) SignatureManagerOption {
	return func(sm *SignatureManager) {
		sm.encoder = encoder
	}
}

func WithSignatureFormatter(formatter SignatureFormatter) SignatureManagerOption {
	return func(sm *SignatureManager) {
		sm.sigFormatter = formatter
	}
}

func WithHeaderFormatter(formatter HeaderFormatter) SignatureManagerOption {
	return func(sm *SignatureManager) {
		sm.headerFormatter = formatter
	}
}

func NewSignatureManager(secrets []WebhookSecret, opts ...SignatureManagerOption) *SignatureManager {
	sm := &SignatureManager{
		secrets:   secrets,
		algorithm: NewHmacSHA256(),
		encoder:   HexEncoder{},
	}

	for _, opt := range opts {
		opt(sm)
	}

	// Formatters must be provided explicitly — no hidden defaults
	if sm.sigFormatter == nil {
		panic("signature content formatter is required — use WithSignatureFormatter()")
	}
	if sm.headerFormatter == nil {
		panic("signature header formatter is required — use WithHeaderFormatter()")
	}

	return sm
}

func (sm *SignatureManager) GenerateSignatures(content SignaturePayload) ([]string, error) {
	if len(sm.secrets) == 0 {
		return nil, nil
	}

	// Sort secrets by creation date, newest first
	sortedSecrets := make([]WebhookSecret, len(sm.secrets))
	copy(sortedSecrets, sm.secrets)
	sort.Slice(sortedSecrets, func(i, j int) bool {
		return sortedSecrets[i].CreatedAt.After(sortedSecrets[j].CreatedAt)
	})

	formattedContent, err := sm.sigFormatter.Format(content)
	if err != nil {
		return nil, err
	}
	var signatures []string
	now := time.Now()

	// Check if latest secret is valid
	latestSecret := sortedSecrets[0]
	if latestSecret.InvalidAt == nil || now.Before(*latestSecret.InvalidAt) {
		signatures = append(signatures, sm.algorithm.Sign(latestSecret.Key, formattedContent, sm.encoder))
	}

	// Add signatures for valid non-latest secrets
	for _, secret := range sortedSecrets[1:] {
		// Check InvalidAt first if it exists
		if secret.InvalidAt != nil {
			if now.After(*secret.InvalidAt) {
				continue
			}
		} else {
			// Fall back to 24-hour window check
			if now.Sub(secret.CreatedAt) >= 24*time.Hour {
				continue
			}
		}
		signatures = append(signatures, sm.algorithm.Sign(secret.Key, formattedContent, sm.encoder))
	}

	return signatures, nil
}

func (sm *SignatureManager) GenerateSignatureHeader(content SignaturePayload) (string, error) {
	signatures, err := sm.GenerateSignatures(content)
	if err != nil {
		return "", err
	}
	if len(signatures) == 0 {
		return "", nil
	}
	return sm.headerFormatter.Format(HeaderPayload{
		EventID:    content.EventID,
		Topic:      content.Topic,
		Timestamp:  content.Timestamp,
		Signatures: signatures,
	})
}

// VerifySignature reports whether signature matches key. It returns false when
// the content template fails to render, since no signature can be verified.
func (sm *SignatureManager) VerifySignature(signature, key string, content SignaturePayload) bool {
	formattedContent, err := sm.sigFormatter.Format(content)
	if err != nil {
		return false
	}
	return sm.algorithm.Verify(key, formattedContent, signature, sm.encoder)
}
