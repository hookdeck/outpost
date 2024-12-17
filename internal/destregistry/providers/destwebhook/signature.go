package destwebhook

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"text/template"
	"time"
)

type SigningAlgorithm interface {
	Sign(key string, content string, encoder SignatureEncoder) string
	Verify(key string, content string, signature string, encoder SignatureEncoder) bool
	Name() string
}

type SignatureFormatter interface {
	Format(timestamp time.Time, body []byte) string
}

type HeaderFormatter interface {
	Format(timestamp time.Time, signatures []string) string
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
	template string
}

func NewSignatureFormatter(template string) *SignatureFormatterImpl {
	if template == "" {
		template = "{{.Timestamp}}.{{.Body}}"
	}
	return &SignatureFormatterImpl{template: template}
}

func (f *SignatureFormatterImpl) fallback(timestamp time.Time, body []byte) string {
	return fmt.Sprintf("%d.%s", timestamp.Unix(), body)
}

func (f *SignatureFormatterImpl) Format(timestamp time.Time, body []byte) string {
	data := struct {
		Timestamp int64
		Body      string
	}{
		Timestamp: timestamp.Unix(),
		Body:      string(body),
	}

	tmpl, err := template.New("signature").Parse(f.template)
	if err != nil {
		return f.fallback(timestamp, body)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return f.fallback(timestamp, body)
	}

	return buf.String()
}

type HeaderFormatterImpl struct {
	template string
}

func NewHeaderFormatter(template string) *HeaderFormatterImpl {
	if template == "" {
		template = "t={{.Timestamp}},v0={{.Signatures}}"
	}
	return &HeaderFormatterImpl{template: template}
}

func (f *HeaderFormatterImpl) fallback(timestamp time.Time, signatures []string) string {
	return fmt.Sprintf("t=%d,v0=%s", timestamp.Unix(), strings.Join(signatures, ","))
}

func (f *HeaderFormatterImpl) Format(timestamp time.Time, signatures []string) string {
	data := struct {
		Timestamp  int64
		Signatures string
	}{
		Timestamp:  timestamp.Unix(),
		Signatures: strings.Join(signatures, ","),
	}

	tmpl, err := template.New("header").Parse(f.template)
	if err != nil {
		return f.fallback(timestamp, signatures)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return f.fallback(timestamp, signatures)
	}

	return buf.String()
}

type HmacSHA256 struct{}

func (h HmacSHA256) Name() string {
	return "hmac-sha256"
}

func (h HmacSHA256) Sign(key string, content string, encoder SignatureEncoder) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(content))
	return encoder.Encode(mac.Sum(nil))
}

func (h HmacSHA256) Verify(key string, content string, signature string, encoder SignatureEncoder) bool {
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
		secrets:         secrets,
		algorithm:       HmacSHA256{},
		sigFormatter:    NewSignatureFormatter(""),
		headerFormatter: NewHeaderFormatter(""),
		encoder:         HexEncoder{},
	}

	for _, opt := range opts {
		opt(sm)
	}

	return sm
}

func (sm *SignatureManager) GenerateSignatures(timestamp time.Time, body []byte) []string {
	if len(sm.secrets) == 0 {
		return nil
	}

	// Sort secrets by creation date, newest first
	sortedSecrets := make([]WebhookSecret, len(sm.secrets))
	copy(sortedSecrets, sm.secrets)
	sort.Slice(sortedSecrets, func(i, j int) bool {
		return sortedSecrets[i].CreatedAt.After(sortedSecrets[j].CreatedAt)
	})

	content := sm.sigFormatter.Format(timestamp, body)
	var signatures []string

	// Always use latest secret
	latestSecret := sortedSecrets[0]
	signatures = append(signatures, sm.algorithm.Sign(latestSecret.Key, content, sm.encoder))

	// Add signatures for non-expired secrets that aren't the latest
	now := time.Now()
	for _, secret := range sortedSecrets[1:] {
		if now.Sub(secret.CreatedAt) < 24*time.Hour {
			signatures = append(signatures, sm.algorithm.Sign(secret.Key, content, sm.encoder))
		}
	}

	return signatures
}

func (sm *SignatureManager) GenerateSignatureHeader(timestamp time.Time, body []byte) string {
	signatures := sm.GenerateSignatures(timestamp, body)
	if len(signatures) == 0 {
		return ""
	}
	return sm.headerFormatter.Format(timestamp, signatures)
}

func (sm *SignatureManager) VerifySignature(signature, key string, timestamp time.Time, body []byte) bool {
	content := sm.sigFormatter.Format(timestamp, body)
	return sm.algorithm.Verify(key, content, signature, sm.encoder)
}
