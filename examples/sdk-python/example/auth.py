import os
import sys
from dotenv import load_dotenv
from outpost_sdk import Outpost, models


def with_tenant_api_key(outpost: Outpost, tenant_api_key: str, tenant_id: str):
    """0.13.1: use the tenant-scoped API key (from tenants.get_token). List destinations only returns destinations for that tenant."""
    print("--- Running with tenant-scoped API key ---")
    try:
        destinations_res = outpost.destinations.list(tenant_id=tenant_id)
        print("Destinations listed successfully using tenant API key:")
        print(destinations_res)
    except Exception as e:
        err_str = str(e)
        if "401" in err_str or "Unauthorized" in err_str:
            print(
                "List destinations with tenant token returned 401. The server could not verify the JWT — "
                "ensure API_JWT_SECRET is set on the Outpost deployment (see sdks/schemas/README.md)."
            )
        print(f"Error listing destinations with tenant API key: {e}")


def with_admin_api_key(outpost: Outpost, tenant_id: str):
    print("--- Running with Admin API Key ---")
    try:
        try:
            health_res = outpost.health.check()
            print("Health check result:")
            print(health_res)
        except Exception as health_err:
            err_str = str(health_err)
            if "404" in err_str or "healthz" in err_str.lower():
                print("Health endpoint not available (e.g. managed Outpost). Skipping.")
            else:
                raise

        destinations_res = outpost.destinations.list(tenant_id=tenant_id)
        print(
            f"Destinations listed successfully using Admin Key for tenant {tenant_id}:"
        )
        print(destinations_res)

        # List tenants (tenants.list(request) with request object)
        try:
            tenants_res = outpost.tenants.list(request=models.ListTenantsRequest(limit=5))
            if tenants_res and tenants_res.result and tenants_res.result.models is not None:
                print(f"Tenants (first page): {len(tenants_res.result.models)} tenant(s)")
            else:
                print("Tenants (first page): (no tenants or 501 if RediSearch not available)")
        except Exception as list_err:
            print(f"List tenants skipped or failed: {list_err}")

        token_res = outpost.tenants.get_token(tenant_id=tenant_id)
        print(f"Tenant token obtained for tenant {tenant_id}:")
        print(token_res)

        if token_res and hasattr(token_res, "token") and token_res.token:
            server_url = outpost.sdk_configuration.server_url
            # 0.13.1: tenant-scoped API key is used as the API key
            tenant_client = Outpost(api_key=token_res.token, server_url=server_url)
            try:
                with_tenant_api_key(tenant_client, token_res.token, tenant_id)
            finally:
                if hasattr(tenant_client, "close"):
                    tenant_client.close()
        else:
            print("Could not obtain tenant token.")

    except Exception as e:
        print(f"Error during admin operations: {e}")


def run():
    load_dotenv()

    admin_api_key = os.getenv("ADMIN_API_KEY")
    tenant_id = os.getenv("TENANT_ID")

    if not admin_api_key:
        print("Error: ADMIN_API_KEY not set.")
        sys.exit(1)
    if not tenant_id:
        print("Error: TENANT_ID not set.")
        sys.exit(1)

    # API_BASE_URL when set (e.g. live), else SERVER_URL + /api/v1
    api_server_url = os.getenv("API_BASE_URL")
    if not api_server_url:
        server_url = os.getenv("SERVER_URL", "http://localhost:3333")
        api_server_url = f"{server_url}/api/v1"

    # 0.13.1: api_key (Admin API Key or tenant-scoped API key)
    outpost = Outpost(api_key=admin_api_key, server_url=api_server_url)
    try:
        with_admin_api_key(outpost, tenant_id)
    finally:
        if hasattr(outpost, "close"):
            outpost.close()

    print("--- Example finished ---")
