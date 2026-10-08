# Migration 004: RSMQ Hash Tags

Moves scheduled retries from the untagged rsmq queue keys to hash-tagged keys.

## Overview

rsmq stores a queue in two keys: a sorted set of message IDs scored by due time, and a hash with the queue settings and message bodies. Every receive, send and delete touches both in one script or transaction. On Redis Cluster the untagged keys hash to different slots, so those operations fail with CROSSSLOT. The hash tag keeps both keys of a queue in one slot.

**Key format change** (both `deliverymq-retry` and `deliverymq-retry-dlq`):
- `rsmq:deliverymq-retry` → `rsmq:{deliverymq-retry}`
- `rsmq:deliverymq-retry:Q` → `rsmq:{deliverymq-retry}:Q`

With `DEPLOYMENT_ID` (e.g. `dp_001`), the deployment ID also goes inside the tag:
- `dp_001:rsmq:deliverymq-retry` → `dp_001:rsmq:{dp_001:deliverymq-retry}`
- `dp_001:rsmq:deliverymq-retry:Q` → `dp_001:rsmq:{dp_001:deliverymq-retry}:Q`

`rsmq:QUEUES` (`dp_001:rsmq:QUEUES`) is unchanged.

## Migration Phases

### Plan
Counts the messages under the untagged keys of each queue.

### Apply
Per queue, a Lua script moves messages in batches of 1000 (each batch atomic):
1. Copies the queue settings (`vt`, `delay`, `maxsize`, `created`, `modified`) to the tagged hash, only where the tagged hash doesn't have them.
2. Moves each message: sorted set member with its score, and the hash fields `<id>`, `<id>:rc`, `<id>:fr`. Messages currently being processed (hidden) keep their score, so they become due at the same time as before.
3. If the tagged queue already holds the same message ID, keeps the tagged one and drops the old one.

The untagged hash keeps its queue settings, so instances still running the previous version see a valid, empty queue and keep working without errors. Retries they schedule from then on stay under the untagged keys until Cleanup.

### Verify
Reports messages still under the untagged keys (scheduled by instances of the previous version after Apply).

### Cleanup
Run once no instance of the previous version is left (`outpost migrate cleanup 004_rsmq_hash_tags`):
1. Moves messages scheduled under the untagged keys since Apply, the same way as Apply (newest wins: a message ID already in the tagged queue is dropped from the untagged one).
2. Deletes the untagged keys of both queues, in the same script as the last batch.

## During a Rolling Upgrade

- A retry an instance of the previous version was processing during Apply, or one it scheduled during the rollout, can be delivered twice (at-least-once). Stopping the previous version before Apply avoids it.
- Without Cleanup, retries scheduled by the previous version during the rollout never fire.

## Redis Cluster

Not applicable: marked `not_applicable`. On Redis Cluster the untagged queues never worked (every receive failed with CROSSSLOT), and the untagged keys can't be read atomically.

## Notes

- Idempotent: running Apply or Cleanup again moves only what is left.
- Doesn't touch any other keys.
