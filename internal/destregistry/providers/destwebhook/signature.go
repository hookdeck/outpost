package destwebhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"
)

type SigningAlgorithm interface {
	Sign(key string, content string) string
	Name() string
}

type SignatureFormatter interface {
	Format(timestamp time.Time, body []byte) string
}

type HeaderFormatter interface {
	FormatHeader(timestamp time.Time, signatures []string) string
}

type DefaultSignatureFormatter struct{}

func (f DefaultSignatureFormatter) Format(timestamp time.Time, body []byte) string {
	return fmt.Sprintf("%d.%s", timestamp.Unix(), body)
}

type DefaultHeaderFormatter struct{}

func (f DefaultHeaderFormatter) FormatHeader(timestamp time.Time, signatures []string) string {
	parts := []string{fmt.Sprintf("t=%d", timestamp.Unix())}
	parts = append(parts, fmt.Sprintf("v0=%s", strings.Join(signatures, ",")))
	return strings.Join(parts, ",")
}

type HmacSHA256 struct{}

func (h HmacSHA256) Name() string {
	return "hmac-sha256"
}

func (h HmacSHA256) Sign(key string, content string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(content))
	return hex.EncodeToString(mac.Sum(nil))
}

type SignatureManager struct {
	secrets         []WebhookSecret
	algorithm       SigningAlgorithm
	sigFormatter    SignatureFormatter
	headerFormatter HeaderFormatter
}

func NewSignatureManager(secrets []WebhookSecret) *SignatureManager {
	return &SignatureManager{
		secrets:         secrets,
		algorithm:       HmacSHA256{},
		sigFormatter:    DefaultSignatureFormatter{},
		headerFormatter: DefaultHeaderFormatter{},
	}
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
	signatures = append(signatures, sm.algorithm.Sign(latestSecret.Key, content))

	// Add signatures for non-expired secrets that aren't the latest
	now := time.Now()
	for _, secret := range sortedSecrets[1:] {
		if now.Sub(secret.CreatedAt) < 24*time.Hour {
			signatures = append(signatures, sm.algorithm.Sign(secret.Key, content))
		}
	}

	return signatures
}

func (sm *SignatureManager) GenerateSignatureHeader(timestamp time.Time, body []byte) string {
	signatures := sm.GenerateSignatures(timestamp, body)
	if len(signatures) == 0 {
		return ""
	}
	return sm.headerFormatter.FormatHeader(timestamp, signatures)
}
