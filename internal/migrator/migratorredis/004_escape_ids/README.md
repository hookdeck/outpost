# Migration 004: Escape IDs

Renames the tenant store keys of tenants and destinations whose ID contains `%`, `:`, `{` or `}`.

## Overview

Tenant and destination IDs are free-form, and the tenant store used to put them into its keys as they are. A `}` in a tenant ID ended the `{...}` part of the key early, so the keys of two different tenants could be the same key.

IDs in keys are now percent-encoded (`internal/rediskey`). This migration moves existing records to the new keys:

| Record | Old key | New key |
|---|---|---|
| Tenant | `tenant:{org:1}:tenant` | `tenant:{org%3A1}:tenant` |
| Destination list | `tenant:{org:1}:destinations` | `tenant:{org%3A1}:destinations` |
| Destination | `tenant:{org:1}:destination:a:b` | `tenant:{org%3A1}:destination:a%3Ab` |

IDs without those four characters have the same key in both formats. Their records are not touched.

## Does it affect my deployment?

Run `outpost migrate plan` with the new version. It prints the number of keys the migration would move:

```
Redis Migrations (1 pending):
  - redis/004_escape_ids: Escape tenant and destination IDs in tenant store keys
      keys_to_move: 0
```

- `keys_to_move: 0`: no tenant or destination ID contains those characters. Run `outpost migrate apply` as for any upgrade; the server doesn't start while a migration is pending.
- Anything else: read the next section before you upgrade.

## Upgrading when keys move

The new version only reads the new keys, and the previous version only reads the old ones. Once the migration has run, an instance still on the previous version no longer finds the moved tenants and destinations:

- API requests for them return 404.
- `PUT /tenants/{id}` for a moved tenant creates a new, empty tenant under the old key, which the new version doesn't see. The same goes for a destination created on a tenant whose keys are being moved.
- Events published for a moved tenant are accepted (202), match no destination and are not delivered.

Tenants and destinations whose keys don't move keep working on both versions.

Pick one:

- **Stop the previous version first.** Stop every instance, run `outpost migrate apply`, start the new version. Nothing is lost; the API is down for the time in between.
- **Rolling update.** Run `outpost migrate apply`, then replace the instances. Until the last instance on the previous version has stopped, events published through it for a moved tenant are dropped. Do it when those tenants publish little, don't create or update them or their destinations until the last instance on the previous version has stopped, and re-publish what was sent in that window.

## Short-lived state

For IDs with those characters, the consecutive-failure streak, the exhausted-retries alert window and the idempotency checks start over once. An event handled once before and once after the upgrade can be delivered twice, and a retry scheduled before the upgrade is not cancelled by a manual retry after it.

`IDGEN_EVENT_PREFIX` and `IDGEN_DESTINATION_PREFIX` are added to generated IDs as they are. If either contains one of those characters, every generated ID has it.

## Safe to run again

A run stops at the first rename that fails, and the migration stays pending. Running it again continues where it stopped: a rename is skipped when its old key is gone, and IDs are read from the stored records.

## Notes

- Deleted destinations are not listed on their tenant. Their records stay under the old key until they expire.
- On Redis Cluster the new key can hash to another slot. The record is then copied and the old key deleted, with its expiry kept. Keys are found with `SCAN`, as in migrations 002 and 003.
