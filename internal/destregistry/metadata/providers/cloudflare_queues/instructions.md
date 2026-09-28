# Cloudflare Queues Configuration Instructions

[Cloudflare Queues](https://developers.cloudflare.com/queues/) is a global message queue that integrates natively with Cloudflare Workers. It enables you to:

- Send and receive messages with guaranteed delivery
- Process messages asynchronously using Workers
- Build reliable, distributed architectures
- Scale automatically with no capacity planning

## Prerequisites

- **Cloudflare Account**: Queues is available on the Workers Free plan (fixed 24-hour message retention) and on Workers Paid
- **Wrangler CLI** (optional): Install via `npm install -g wrangler` for CLI-based setup

## How to Find Your Account ID

Your Cloudflare Account ID is required for API access.

### Via Dashboard

1. Log in to the [Cloudflare Dashboard](https://dash.cloudflare.com/)
2. Select your account
3. The Account ID is displayed in the URL: `https://dash.cloudflare.com/<ACCOUNT_ID>/...`
4. Alternatively, go to **Workers & Pages** > **Overview** and find the Account ID in the right sidebar

### Via Wrangler CLI

```bash
# Authenticate with Cloudflare
npx wrangler login

# List accounts and their IDs
npx wrangler whoami
```

## How to Create a Queue

### Via Dashboard

1. Log in to the [Cloudflare Dashboard](https://dash.cloudflare.com/)
2. Navigate to **Workers & Pages** > **Queues**
3. Click **Create Queue**
4. Enter a name for your queue
5. Click **Create**
6. Copy the **Queue ID** from the queue details page

### Via Wrangler CLI

```bash
# Create a new queue
npx wrangler queues create my-queue

# List all queues to get the Queue ID
npx wrangler queues list
```

The output will show your queue with its ID:

```
┌──────────────────────────────────┬──────────┐
│ id                               │ name     │
├──────────────────────────────────┼──────────┤
│ 9d7d4cf8a3a14d9aaeb50c3e74e2f4b1 │ my-queue │
└──────────────────────────────────┴──────────┘
```

## How to Create an API Token

You need a Cloudflare API Token with permissions to write to Queues.

### Via Dashboard

1. Go to [Cloudflare API Tokens](https://dash.cloudflare.com/profile/api-tokens)
2. Click **Create Token**
3. Select **Create Custom Token**
4. Configure the token:
   - **Token name**: e.g., "Outpost Queues Publisher"
   - **Permissions**:
     - Account > Queues > Edit
   - **Account Resources**:
     - Include > Your Account (or specific account)
5. Click **Continue to summary**
6. Click **Create Token**
7. Copy the token immediately (it won't be shown again)

### Permission Details

The API Token requires the following permission:
- **Account** > **Queues** > **Edit** (API name: Queues Write), which allows sending messages to queues

## Configuration

When configuring your Cloudflare Queues destination, you'll need:

1. **Account ID**: Your Cloudflare Account ID (32-character hex string)
2. **Queue ID**: The ID of your Cloudflare Queue (32-character hex string, not the queue name)
3. **API Token**: A Cloudflare API Token with the Account > Queues > Edit (Queues Write) permission

Account ID and Queue ID must be 32-character lowercase hex strings, as Cloudflare shows them. Outpost rejects anything else, such as the queue name, a dashed UUID or uppercase hex, when the destination is created or updated.

## Message Format

Each event is published as a single Cloudflare Queue message (128 KB maximum) with the following request body:

```json
{
  "body": {
    "data": <event.Data>,
    "metadata": <merged metadata>
  },
  "content_type": "json"
}
```

`content_type: "json"` tells Cloudflare to deliver the body as a parsed object to consumer Workers. Messages are sent via [`POST /accounts/{account_id}/queues/{queue_id}/messages`](https://developers.cloudflare.com/api/resources/queues/subresources/messages/methods/push/).

## Testing the Integration

### Create a Consumer Worker

To verify messages are being delivered, create a simple consumer Worker:

```javascript
export default {
  async queue(batch, env) {
    for (const message of batch.messages) {
      console.log('Received message:', JSON.stringify(message.body));
      message.ack();
    }
  },
};
```

Deploy with wrangler.toml:

```toml
name = "queue-consumer"
main = "src/index.js"

[[queues.consumers]]
queue = "my-queue"
max_batch_size = 10
max_batch_timeout = 30
```

### View Queue Metrics

1. Go to the [Cloudflare Dashboard](https://dash.cloudflare.com/)
2. Navigate to **Workers & Pages** > **Queues**
3. Select your queue
4. View metrics for messages sent, delivered, and acknowledged

## Troubleshooting

### Authentication Errors (401)

Cloudflare returns `401 Authentication error` for all of these:

- The API Token is wrong, expired or revoked
- The token lacks the **Account > Queues > Edit** (Queues Write) permission, for example a token with Queues Read only
- The token isn't scoped to the account in the Account ID

### Queue Not Found (500)

- Cloudflare returns `500 Unknown Internal Error` for a well-formed Queue ID that doesn't exist in the account
- Verify the Queue ID with `npx wrangler queues list` and check the Account ID matches where the queue was created

### Rate Limiting (429)

Cloudflare limits each queue to 5,000 messages per second. Throttled attempts fail and Outpost retries them according to its retry policy. See [Cloudflare Queues limits](https://developers.cloudflare.com/queues/platform/limits/).

### Message Too Large

Cloudflare rejects messages larger than 128 KB (128,000 bytes), including the `data` and `metadata` wrapper Outpost adds, with a `413` response. Outpost does not check the size before sending, so an oversized event fails on every attempt.

## Additional Resources

- [Cloudflare Queues Documentation](https://developers.cloudflare.com/queues/)
- [Queues REST API Reference](https://developers.cloudflare.com/api/resources/queues/subresources/messages/methods/push/)
- [Cloudflare API Tokens](https://developers.cloudflare.com/fundamentals/api/get-started/create-token/)
- [Wrangler CLI Documentation](https://developers.cloudflare.com/workers/wrangler/)
- [Queues Pricing](https://developers.cloudflare.com/queues/platform/pricing/)
