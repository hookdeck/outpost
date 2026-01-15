# General walkthough

```sh
TENANT_ID=your_org_name
API_KEY=apikey
URL=your_webhook_url
```

## Create a Tenant

You'd do this whenever a new organization signups up.

```sh
curl --location --request PUT "localhost:3333/api/v1/tenants/$TENANT_ID" \
--header "Authorization: Bearer $API_KEY"
```

## Create a Destination

When someone within an org wants to subscribe to an event, create a Destination:

```sh
curl --location "localhost:3333/api/v1/tenants/$TENANT_ID/destinations" \
--header "Content-Type: application/json" \
--header "Authorization: Bearer $API_KEY" \
--data '{
    "type": "webhook",
    "topics": ["user.created", "user.updated", "user.deleted"],
    "config": {
        "url": "'"$URL"'"
    }
}'
```

`type` can be `webhook`, `awssqs`, `rabbitmq`...

`topics` are used to provide more granular subscription

`config` differs depending on the `type`.

`credentials`, not used in the example, also differ depending on the `type`.

## Publish an event

Whenever and event in your system occurs, you publish an event.

Outpost supports two ways of publishing:

1. Via the API
2. Via a publish queue

Publish via the API:

```sh
curl --location "localhost:3333/api/v1/publish" \
--header "Content-Type: application/json" \
--header "Authorization: Bearer $API_KEY" \
--data '{
    "tenant_id": "'"$TENANT_ID"'",
    "topic": "user.created",
    "eligible_for_retry": true,
    "metadata": {
        "meta": "data"
    },
    "data": {
        "user_id": "userid"
    }
}'
```

Check the webhook was delivered and ingested.

## Open the Outpost Portal

Outpost comes with a pre-built portal that supports event destination management, event inspection, and metrics.

Event Inspection will be available next week (mid-March). Metric will be part of the BETA release.

```sh
curl "localhost:3333/api/v1/tenants/$TENANT_ID/portal" \
--header "Authorization: Bearer $API_KEY"
```

## Create a new destination in the portal

- Use something like Hookdeck to create a new ingestion endpoint
- Add authentication handling to the ingestion endpoint
- Pubish a new event and ensure the ingestion endpoint verified the request
