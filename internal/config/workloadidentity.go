package config

import (
	"errors"
	"fmt"
	"os"

	"github.com/hookdeck/outpost/internal/workloadidentity"
)

// WorkloadIdentityConfig turns Outpost into an OIDC issuer so destinations can
// authenticate to cloud providers through workload identity federation
// instead of stored keys. Disabled unless issuer and a signing key are set.
type WorkloadIdentityConfig struct {
	Issuer               string `yaml:"issuer" env:"WORKLOAD_IDENTITY_ISSUER" desc:"Public HTTPS URL of the workload identity issuer: the 'iss' claim of tokens Outpost signs for destinations, and the URL cloud providers fetch '/.well-known/openid-configuration' and '/jwks.json' under. The API serves both at '/workload-identity', so this is usually 'https://<api-host>/workload-identity'. Setting it together with a signing key enables workload identity federation for destinations." required:"N"`
	SigningKey           string `yaml:"signing_key" env:"WORKLOAD_IDENTITY_SIGNING_KEY" desc:"PEM private key (RSA, at least 2048 bits, or EC P-256) used to sign workload identity tokens. Literal '\\n' sequences are accepted in place of newlines. Mutually exclusive with signing_key_file." required:"N"`
	SigningKeyFile       string `yaml:"signing_key_file" env:"WORKLOAD_IDENTITY_SIGNING_KEY_FILE" desc:"Path to a file holding the PEM signing key. Mutually exclusive with signing_key." required:"N"`
	VerificationKeys     string `yaml:"verification_keys" env:"WORKLOAD_IDENTITY_VERIFICATION_KEYS" desc:"Additional PEM keys (public or private, one or more blocks) whose public part is published in the JWKS next to the signing key. Use it to rotate keys: publish the new key here before signing with it, and keep the old one here until tokens signed with it have expired." required:"N"`
	VerificationKeysFile string `yaml:"verification_keys_file" env:"WORKLOAD_IDENTITY_VERIFICATION_KEYS_FILE" desc:"Path to a file holding the additional PEM keys. Mutually exclusive with verification_keys." required:"N"`
}

var ErrInvalidWorkloadIdentity = errors.New("config validation error: invalid workload_identity")

func (c *WorkloadIdentityConfig) configured() bool {
	return c.Issuer != "" || c.SigningKey != "" || c.SigningKeyFile != "" ||
		c.VerificationKeys != "" || c.VerificationKeysFile != ""
}

// ToConfig resolves the key files. It returns nil when workload identity is
// not configured.
func (c *WorkloadIdentityConfig) ToConfig() (*workloadidentity.Config, error) {
	if !c.configured() {
		return nil, nil
	}
	signingKey, err := valueOrFile("signing_key", c.SigningKey, c.SigningKeyFile)
	if err != nil {
		return nil, err
	}
	verificationKeys, err := valueOrFile("verification_keys", c.VerificationKeys, c.VerificationKeysFile)
	if err != nil {
		return nil, err
	}
	return &workloadidentity.Config{
		Issuer:           c.Issuer,
		SigningKey:       signingKey,
		VerificationKeys: verificationKeys,
	}, nil
}

// Build returns the issuer, or nil when workload identity is not configured.
func (c *WorkloadIdentityConfig) Build() (*workloadidentity.Issuer, error) {
	cfg, err := c.ToConfig()
	if err != nil || cfg == nil {
		return nil, err
	}
	issuer, err := workloadidentity.New(*cfg)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidWorkloadIdentity, err)
	}
	return issuer, nil
}

func (c *WorkloadIdentityConfig) validate() error {
	_, err := c.Build()
	return err
}

func valueOrFile(name, value, path string) (string, error) {
	if value != "" && path != "" {
		return "", fmt.Errorf("%w: set %s or %s_file, not both", ErrInvalidWorkloadIdentity, name, name)
	}
	if path == "" {
		return value, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("%w: reading %s_file: %w", ErrInvalidWorkloadIdentity, name, err)
	}
	return string(data), nil
}
