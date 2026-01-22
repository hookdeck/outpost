# Cloudflare Queues Configuration Instructions

[Cloudflare Queues](https://developers.cloudflare.com/queues/) is a global message queue that integrates natively with Cloudflare Workers. It enables you to:

- Send and receive messages with guaranteed delivery
- Process messages asynchronously using Workers
- Build reliable, distributed architectures
- Scale automatically with no capacity planning

## Prerequisites

- **Cloudflare Account**: A Cloudflare account with a Workers Paid plan (required for Queues)
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
┌──────────────────────────────────────┬──────────┐
│ id                                   │ name     │
├──────────────────────────────────────┼──────────┤
│ 12345678-1234-1234-1234-123456789abc │ my-queue │
└──────────────────────────────────────┴──────────┘
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
- **Account** > **Queues** > **Edit** - This grants `queues:write` access to send messages to queues

## Configuration

When configuring your Cloudflare Queues destination, you'll need:

1. **Account ID**: Your Cloudflare Account ID
2. **Queue ID**: The UUID of your Cloudflare Queue
3. **API Token**: A Cloudflare API Token with Queues write permission

## Message Format

When events are sent to Cloudflare Queues, each message contains:

- **body**: The event payload as a JSON object
- **contentType**: Set to `application/json`

Messages are sent using the [Cloudflare Queues REST API](https://developers.cloudflare.com/api/operations/queue-send-messages).

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

- Verify your API Token is correct and hasn't been revoked
- Ensure the token has **Queues > Edit** permission
- Check the token is scoped to the correct account

### Queue Not Found (404)

- Verify the Queue ID is correct (it's a UUID, not the queue name)
- Ensure the queue exists in the account associated with your API Token
- Check the Account ID matches where the queue was created

### Permission Denied (403)

- Verify your API Token has the **Queues > Edit** permission
- Ensure the token is scoped to the account containing the queue

### Rate Limiting (429)

Cloudflare Queues has rate limits. If you encounter rate limiting:
- Implement backoff/retry logic
- Consider batching messages
- Review [Cloudflare Queues limits](https://developers.cloudflare.com/queues/platform/limits/)

## Additional Resources

- [Cloudflare Queues Documentation](https://developers.cloudflare.com/queues/)
- [Queues REST API Reference](https://developers.cloudflare.com/api/operations/queue-send-messages)
- [Cloudflare API Tokens](https://developers.cloudflare.com/fundamentals/api/get-started/create-token/)
- [Wrangler CLI Documentation](https://developers.cloudflare.com/workers/wrangler/)
- [Queues Pricing](https://developers.cloudflare.com/queues/platform/pricing/)
