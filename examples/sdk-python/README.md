## Outpost Python SDK Example

This example demonstrates using the Outpost Python SDK.

The source code for the Python SDK can be found in the [`sdks/outpost-python/`](../../sdks/outpost-python/) directory.

### Prerequisites

*   Python 3.7+
*   Poetry

> [!NOTE]
> All commands below should be run from within the `examples/sdk-python` directory.

### Setup

1.  **Install dependencies:**
    ```bash
    poetry install
    ```
2.  **Activate the virtual environment:**
    ```bash
    poetry shell
    ```
    *(Run subsequent commands within this activated shell)*

### Running the Example

1.  **Configure environment variables:**
    Create a `.env` file in this directory (`examples/sdk-python`) with the following:
    ```dotenv
    SERVER_URL="your_server_url"
    ADMIN_API_KEY="your_admin_api_key"
    TENANT_ID="your_tenant_id"
    ```
    Replace the placeholder values with your Outpost server URL, Admin API key, and Tenant ID. (Note: `.env` is already gitignored).

2.  **Run the example script:**
    *(Ensure you are inside the Poetry shell activated in the setup step)*

    The `app.py` script is now a command-line interface (CLI) that accepts different commands to run specific examples.

    *   **To run the API Key and JWT client auth example:**
        ```bash
        python app.py auth
        ```
    *   **To run the create destination example:**
        ```bash
        python app.py create-destination
        ```
    *   **To run the publish event example:**
        ```bash
        python app.py publish-event
        ```

    Review the `app.py` file and the `example/` directory for more details on the implementation.