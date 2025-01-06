# RSMQ Fork

This is a fork of [github.com/semihbkgr/go-rsmq](https://github.com/semihbkgr/go-rsmq) v1.3.1 (commit d0f7bbc).

## Changes from Original

The main change in this fork is support for custom message IDs. This allows for:
- Deterministic message IDs based on your own identifiers
- Message deduplication
- Message overriding/rescheduling

### Custom ID Notes

When using custom IDs, be aware that:
- The `msg.Sent` timestamp will not reflect the actual send time for overridden messages
  - This field is derived from the message ID's timestamp part
  - For custom IDs, we use a fixed timestamp to avoid timing issues
  - Use Redis sorted set scores (via the `delay` parameter) for actual timing
- Message timing is controlled by Redis sorted set scores, not by the ID's timestamp part
- IDs must be exactly 32 characters long and contain only alphanumeric characters
- Overriding a message with the same ID but different delay will correctly update the timing

## Original License

MIT License - see original repository for details. 