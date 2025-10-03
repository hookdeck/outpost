## Outpost TypeScript SDK Example

This example demonstrates using the Outpost TypeScript SDK.

The source code for the TypeScript SDK can be found in the [`sdks/outpost-typescript/`](../../sdks/outpost-typescript/) directory.

### Prerequisites

*   Node.js (LTS recommended)
*   NPM

> [!NOTE]
> All commands below should be run from within the `examples/sdk-typescript` directory.

### Setup

1.  **Install dependencies:**
    ```bash
    npm install
    ```

### Running the Example

1.  **Configure environment variables:**
    Create a `.env` file in this directory (`examples/sdk-typescript`) with the following:
    ```dotenv
    SERVER_URL="your_server_url"
    ADMIN_API_KEY="your_admin_api_key"
    TENANT_ID="your_tenant_id"
    ```
    Replace the placeholder values with your Outpost server URL, Admin API key, and Tenant ID. (Note: `.env` is already gitignored).

2.  **Run the example:**
    The example is split into multiple files. You can run each one individually.

    *   **To run the resource management example (from `index.ts`):**
        This example demonstrates creating tenants, destinations, and publishing events.
        ```bash
        npm run start
        ```

    *   **To run the authentication example (from `auth.ts`):**
        This example demonstrates using the Admin API key to fetch a tenant JWT and then using that JWT.
        ```bash
        npm run auth
        ```

    *   **To run the create destination example (from `create-destination.ts`):**
        This example demonstrates creating a destination.
        ```bash
        npm run create-destination
        ```

    *   **To run the publish event example (from `publish-event.ts`):**
        This example demonstrates publishing an event to a topic.
        ```bash
        npm run publish-event
        ```

    Review the respective `.ts` files (`index.ts`, `auth.ts`, `create-destination.ts`, `publish-event.ts`) for details on what each example does.
