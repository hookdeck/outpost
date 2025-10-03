import os
import sys
from dotenv import load_dotenv
from outpost_sdk import Outpost, models


def with_jwt(outpost: Outpost, jwt: str):
    print("--- Running with Tenant JWT ---")
    try:
        destinations_res = outpost.destinations.list()
        print("Destinations listed successfully using JWT:")
        print(destinations_res)
    except Exception as e:
        print(f"Error listing destinations with JWT: {e}")


def with_admin_api_key(outpost: Outpost, tenant_id: str):
    print("--- Running with Admin API Key ---")
    try:
        health_res = outpost.health.check()
        print("Health check result:")
        print(health_res)

        destinations_res = outpost.destinations.list(tenant_id=tenant_id)
        print(
            f"Destinations listed successfully using Admin Key for tenant {tenant_id}:"  # noqa E501
        )
        print(destinations_res)

        token_res = outpost.tenants.get_token(tenant_id=tenant_id)
        print(f"Tenant token obtained for tenant {tenant_id}:")
        print(token_res)

        if token_res and hasattr(token_res, "token") and token_res.token:
            # Re-initialize outpost with JWT for this part of the test
            security_config = models.Security(tenant_jwt=token_res.token)
            with Outpost(
                security=security_config,
                server_url=outpost.sdk_configuration.server_url,
            ) as jwt_outpost:
                with_jwt(jwt_outpost, token_res.token)
        else:
            print("Could not obtain tenant token.")

    except Exception as e:
        print(f"Error during admin operations: {e}")


def run():
    load_dotenv()

    admin_api_key = os.getenv("ADMIN_API_KEY")
    tenant_id = os.getenv("TENANT_ID")
    server_url = os.getenv("SERVER_URL")

    if not admin_api_key:
        print("Error: ADMIN_API_KEY not set.")
        sys.exit(1)
    if not tenant_id:
        print("Error: TENANT_ID not set.")
        sys.exit(1)
    if not server_url:
        print("Error: SERVER_URL not set.")
        sys.exit(1)

    api_server_url = f"{server_url}/api/v1"

    security_config = models.Security(admin_api_key=admin_api_key)
    with Outpost(security=security_config, server_url=api_server_url) as outpost:
        with_admin_api_key(outpost, tenant_id)

    print("--- Example finished ---")
