# Example Webhooks Migration Script

Create a `.env`:

```
# Base API URL of your Outpost installation
# Include the API and version path
# e.g. http://localhost:3333/api/v1
OUTPOST_API_BASE_URL=

# API Key for your Outpost installation
OUTPOST_API_KEY=

# A webhook endpoint to test the receipt of
# webhook events
REAL_TEST_ENDPOINT=https://hkdk.events/bmqKUtADjRzR
```

## Example migration script

`src/migrate.ts`

Demonstrates how a migration script could work.

Run with:

```
npm run migrate
```

## Example publish test script

Following the migration you may want to test publishing an event and it being received by a destination.

Uses the `REAL_TEST_ENDPOINT` value to identify what to publish.

`src/publish-test.ts`

Run with:

```
npm run publish-test
```

## Example verification script

As part of ensuring events are delivered as expected you should also ensure that webhook signatures are in the expected formation.

This script provides two examples of webhook verification.

`src/verify-signature.ts`

Run with:

```
npm run verify
```
