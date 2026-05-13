package e2e_test

import (
	"time"
)

func (s *basicSuite) TestWebhookSignatures_RotatedSecretAcceptedDuringGracePeriod() {
	tenant := s.createTenant()
	secret := testSecret
	newSecret := testSecretAlt
	dest := s.createWebhookDestination(tenant.ID, "*", withSecret(secret))

	// Rotate secret on mock server: mock now verifies with new secret + previous secret
	dest.SetCredentials(s, map[string]string{
		"secret":                     newSecret,
		"previous_secret":            secret,
		"previous_secret_invalid_at": time.Now().Add(24 * time.Hour).Format(time.RFC3339),
	})

	// Publish — outpost still signs with original secret, mock's previous_secret should match
	s.publish(tenant.ID, "user.created", map[string]any{
		"event_id": "rotated_test",
	})

	events := s.waitForNewMockServerEvents(dest.mockID, 1)
	s.Require().Len(events, 1)
	s.True(events[0].Verified, "signature should be verified via previous_secret during grace period")
}

func (s *basicSuite) TestWebhookSignatures_WrongSecretFailsVerification() {
	tenant := s.createTenant()
	secret := testSecret
	dest := s.createWebhookDestination(tenant.ID, "*", withSecret(secret))

	// Set wrong secret on mock server
	dest.SetSecret(s, "wrong-secret")

	s.publish(tenant.ID, "user.created", map[string]any{
		"event_id": "wrong_secret_test",
	})

	events := s.waitForNewMockServerEvents(dest.mockID, 1)
	s.Require().Len(events, 1)
	s.True(events[0].Success, "delivery should still succeed")
	s.False(events[0].Verified, "signature should NOT be verified with wrong secret")
}
