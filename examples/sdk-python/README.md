## Outpost Python SDK Example

This example demonstrates using the Outpost Python SDK.

The source code for the Python SDK can be found in the [`sdks/outpost-python/`](../../sdks/outpost-python/) directory. This example uses the **locally built** SDK (path dependency in `pyproject.toml`) and targets **Outpost 0.13.1**.

### Prerequisites

*   Python 3.7+
*   Poetry

> [!NOTE]
> All commands below should be run from within the `examples/sdk-python` directory.

### Setup

**Option A — Using the run script (recommended if Poetry has issues)**  
From `examples/sdk-python`, create a `.env` (see below), then:
```bash
./run-auth.sh
```
This creates a `.venv`, installs the local SDK and dependencies, and runs the auth example. For other commands (e.g. create-destination), activate the venv and run:
```bash
.venv/bin/python app.py create-destination
```

**Option B — Using Poetry**

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
    API_BASE_URL="https://api.outpost.hookdeck.com/2025-07-01"
    # Or for local: SERVER_URL="http://localhost:3333"
    ADMIN_API_KEY="your_admin_api_key"
    TENANT_ID="your_tenant_id"
    ```
    Use `API_BASE_URL` for the full API base, or `SERVER_URL` for local. (Note: `.env` is gitignored.)

2.  **Run the example script:**  
    If you used **Option A** (run script), use `./run-auth.sh` or `.venv/bin/python app.py auth`.  
    If you used **Option B** (Poetry), ensure you are inside the Poetry shell, then use `python app.py auth`.

    The `app.py` script is a command-line interface (CLI) that accepts different commands:

    *   **To run the API Key and tenant-scoped API key auth example:**
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