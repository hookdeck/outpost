## Outpost Go SDK Example

This example demonstrates using the Outpost Go SDK. It is structured into a few files:
*   `main.go`: Handles command-line arguments to select which example to run.
*   `auth.go`: Contains examples related to authentication and tenant JWTs.
*   `resources.go`: Contains examples for managing Outpost resources like tenants and destinations.
*   `create_destination.go`: Contains an example for creating a destination.
*   `publish_event.go`: Contains an example for publishing an event.

The source code for the Go SDK can be found in the [`sdks/outpost-go/`](../../sdks/outpost-go/) directory.


### Prerequisites

*   Go (version specified in `go.mod`)

> [!NOTE]
> All commands below should be run from within the `examples/sdk-go` directory.

### Setup

1.  **Download dependencies:**
    *(This will also add the `godotenv` package needed for `.env` file loading)*
    ```bash
    go mod tidy
    ```

### Running the Examples

1.  **Configure environment variables:**
    Create a `.env` file in this directory (`examples/sdk-go`) with the following:
    ```dotenv
    SERVER_URL="your_outpost_server_url"
    ADMIN_API_KEY="your_admin_api_key"
    TENANT_ID="your_tenant_id"
    ```
    Replace the placeholder values with your Outpost server URL, Admin API key, and a Tenant ID to use for the examples.
    (Note: You'll need to create a `.gitignore` file if you don't want to commit `.env`).

    *   `SERVER_URL`: The base URL of your Outpost server (e.g., `http://localhost:3333`).
    *   `ADMIN_API_KEY`: Your Outpost Admin API key.
    *   `TENANT_ID`: An identifier for a tenant (e.g., `my_organization`). This is used by the `auth` example and parts of the `manage` example.

2.  **Run a specific example:**
    The `main.go` program now accepts an argument to specify which example to run.

    *   **To run the resource management example (from `resources.go`):**
        This example demonstrates creating tenants, destinations, and publishing events.
        ```bash
        go run . manage
        ```

    *   **To run the authentication example (from `auth.go`):**
        This example demonstrates using the Admin API key to fetch a tenant JWT and then using that JWT.
        ```bash
        go run . auth
        ```

    *   **To run the create destination example (from `create_destination.go`):**
        This example demonstrates creating a destination.
        ```bash
        go run . create-destination
        ```

   *   **To run the publish event example (from `publish_event.go`):**
       This example demonstrates publishing an event to a topic.
       ```bash
       go run . publish-event
       ```

    If you run `go run .` without an argument, or with an unknown argument, it will display a usage message.
    Review the respective `.go` files (`auth.go`, `resources.go`, `main.go`, `create_destination.go`, `publish_event.go`) for details on what each example does.