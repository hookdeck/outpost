# Cloudflare Queues Configuration Instructions

[Cloudflare Queues](https://developers.cloudflare.com/queues/) is a global message queue that integrates natively with Cloudflare Workers. Queues is available on the Workers Free plan (fixed 24-hour message retention) and on Workers Paid.

## How to configure Cloudflare Queues as an event destination

1. Find your Account ID

    Log in to the [Cloudflare Dashboard](https://dash.cloudflare.com/) and select your account. The Account ID is in the URL: `https://dash.cloudflare.com/<ACCOUNT_ID>/...`. With the [Wrangler CLI](https://developers.cloudflare.com/workers/wrangler/), run `npx wrangler whoami`.

2. Create a queue and copy its Queue ID

    ```sh
    npx wrangler queues create my-queue
    npx wrangler queues list
    ```

    The Queue ID is the `id` column, for example `9d7d4cf8a3a14d9aaeb50c3e74e2f4b1`. You can also create the queue in the dashboard and copy the Queue ID from its details page.

3. Create an API Token

    Go to [API Tokens](https://dash.cloudflare.com/profile/api-tokens), click **Create Token**, then **Create Custom Token**:

    - **Permissions**: Account > Queues > Edit (API name: Queues Write)
    - **Account Resources**: Include > the account that owns the queue

    Copy the token after creating it. It isn't shown again.

4. Configure your Cloudflare Queues event destination

    Use the Account ID, Queue ID and API Token from the steps above. Account ID and Queue ID must be 32-character lowercase hex strings, as Cloudflare shows them. The queue name, a dashed UUID or uppercase hex is rejected.
