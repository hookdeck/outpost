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
	"strings"
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
	Format(content SignaturePayload) string
}

type HeaderFormatter interface {
	Format(content HeaderPayload) string
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

func NewSignatureFormatter(templateStr string) *SignatureFormatterImpl {
	if templateStr == "" {
		templateStr = `{{.Timestamp.Unix}}.{{.Body}}`
	}

	tmpl := template.New("signature").Funcs(sprig.TxtFuncMap())

	// Parse template, fallback to default if fails
	parsed, err := tmpl.Parse(templateStr)
	if err != nil {
		parsed, _ = tmpl.Parse(`{{.Timestamp.Unix}}.{{.Body}}`)
	}

	return &SignatureFormatterImpl{template: parsed}
}

func (f *SignatureFormatterImpl) fallback(content SignaturePayload) string {
	return fmt.Sprintf("%d.%s", content.Timestamp.Unix(), content.Body)
}

func (f *SignatureFormatterImpl) Format(content SignaturePayload) string {
	var buf bytes.Buffer
	if err := f.template.Execute(&buf, content); err != nil {
		return f.fallback(content)
	}
	return buf.String()
}

type HeaderFormatterImpl struct {
	template *template.Template
}

func NewHeaderFormatter(templateStr string) *HeaderFormatterImpl {
	if templateStr == "" {
		templateStr = `t={{.Timestamp.Unix}},v0={{.Signatures | join ","}}`
	}

	tmpl := template.New("header").Funcs(sprig.TxtFuncMap())

	// Parse template, fallback to default if fails
	parsed, err := tmpl.Parse(templateStr)
	if err != nil {
		parsed, _ = tmpl.Parse(`t={{.Timestamp.Unix}},v0={{.Signatures | join ","}}`)
	}

	return &HeaderFormatterImpl{template: parsed}
}

func (f *HeaderFormatterImpl) fallback(content HeaderPayload) string {
	return fmt.Sprintf("t=%d,v0=%s", content.Timestamp.Unix(), strings.Join(content.Signatures, ","))
}

func (f *HeaderFormatterImpl) Format(content HeaderPayload) string {
	var buf bytes.Buffer
	if err := f.template.Execute(&buf, content); err != nil {
		return f.fallback(content)
	}
	return buf.String()
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
		secrets:         secrets,
		algorithm:       NewHmacSHA256(),
		sigFormatter:    NewSignatureFormatter(""),
		headerFormatter: NewHeaderFormatter(""),
		encoder:         HexEncoder{},
	}

	for _, opt := range opts {
		opt(sm)
	}

	return sm
}

func (sm *SignatureManager) GenerateSignatures(content SignaturePayload) []string {
	if len(sm.secrets) == 0 {
		return nil
	}

	// Sort secrets by creation date, newest first
	sortedSecrets := make([]WebhookSecret, len(sm.secrets))
	copy(sortedSecrets, sm.secrets)
	sort.Slice(sortedSecrets, func(i, j int) bool {
		return sortedSecrets[i].CreatedAt.After(sortedSecrets[j].CreatedAt)
	})

	formattedContent := sm.sigFormatter.Format(content)
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

	return signatures
}

func (sm *SignatureManager) GenerateSignatureHeader(content SignaturePayload) string {
	signatures := sm.GenerateSignatures(content)
	if len(signatures) == 0 {
		return ""
	}
	return sm.headerFormatter.Format(HeaderPayload{
		EventID:    content.EventID,
		Topic:      content.Topic,
		Timestamp:  content.Timestamp,
		Signatures: signatures,
	})
}

func (sm *SignatureManager) VerifySignature(signature, key string, content SignaturePayload) bool {
	formattedContent := sm.sigFormatter.Format(content)
	return sm.algorithm.Verify(key, formattedContent, signature, sm.encoder)
}
