package deliverymq

import (
	"errors"
	"time"

	"github.com/hookdeck/outpost/internal/logging"
	"github.com/hookdeck/outpost/internal/rsmq"
	"go.uber.org/zap"
)

// legacyRecheckInterval is how long the legacy queue goes unpolled after it
// was found empty (or failed). A retry written there in the meantime, e.g. by
// an older instance during a rolling upgrade, fires up to this much late.
const legacyRecheckInterval = time.Minute

// legacyDrainClient drains retries scheduled by versions that stored the retry
// queue under untagged keys. It receives from the legacy queue as well as the
// current one and deletes from both, but never writes to the legacy queue.
// Transitional: remove once deployments no longer hold untagged retry keys.
type legacyDrainClient struct {
	rsmq.Client
	legacy rsmq.Client
	logger *logging.Logger
	now    func() time.Time

	// skipLegacyUntil is only accessed from ReceiveMessagePoll, which the
	// scheduler calls from its single Monitor goroutine.
	skipLegacyUntil time.Time
}

func newLegacyDrainClient(client, legacy rsmq.Client, logger *logging.Logger) *legacyDrainClient {
	return &legacyDrainClient{Client: client, legacy: legacy, logger: logger, now: time.Now}
}

func (c *legacyDrainClient) ReceiveMessagePoll(qname string, vt uint) (rsmq.PollResult, error) {
	legacyRes := c.pollLegacy(qname, vt)
	if legacyRes.Message != nil {
		return legacyRes, nil
	}

	res, err := c.Client.ReceiveMessagePoll(qname, vt)
	if err != nil || res.Message != nil || !legacyRes.HasNext {
		return res, err
	}
	if !res.HasNext || legacyRes.NextDue < res.NextDue {
		res.HasNext = true
		res.NextDue = legacyRes.NextDue
	}
	return res, nil
}

// pollLegacy polls the legacy queue unless it was recently found empty. Errors
// are logged and treated as empty so they never stop the current queue.
func (c *legacyDrainClient) pollLegacy(qname string, vt uint) rsmq.PollResult {
	now := c.now()
	if now.Before(c.skipLegacyUntil) {
		return rsmq.PollResult{}
	}
	res, err := c.legacy.ReceiveMessagePoll(qname, vt)
	if err != nil && !errors.Is(err, rsmq.ErrQueueNotFound) {
		if c.logger != nil {
			c.logger.Warn("legacy retry queue receive error",
				zap.Error(err),
				zap.Duration("recheck_in", legacyRecheckInterval))
		}
		res = rsmq.PollResult{}
	}
	if res.Message == nil && !res.HasNext {
		c.skipLegacyUntil = now.Add(legacyRecheckInterval)
	}
	return res
}

func (c *legacyDrainClient) ChangeMessageVisibility(qname string, id string, vt uint) error {
	err := c.Client.ChangeMessageVisibility(qname, id, vt)
	if !errors.Is(err, rsmq.ErrMessageNotFound) {
		return err
	}
	if legacyErr := c.legacy.ChangeMessageVisibility(qname, id, vt); !isNotFound(legacyErr) {
		return legacyErr
	}
	return err
}

func (c *legacyDrainClient) DeleteMessage(qname string, id string) error {
	err := c.Client.DeleteMessage(qname, id)
	if err != nil && !errors.Is(err, rsmq.ErrMessageNotFound) {
		return err
	}
	legacyErr := c.legacy.DeleteMessage(qname, id)
	if isNotFound(legacyErr) {
		return err
	}
	if legacyErr != nil && err == nil {
		// Deleted from the current queue; a legacy-only failure must not fail it.
		if c.logger != nil {
			c.logger.Warn("legacy retry queue delete error", zap.Error(legacyErr), zap.String("id", id))
		}
		return nil
	}
	return legacyErr
}

func isNotFound(err error) bool {
	return errors.Is(err, rsmq.ErrMessageNotFound) || errors.Is(err, rsmq.ErrQueueNotFound)
}
