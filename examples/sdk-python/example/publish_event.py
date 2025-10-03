import os
from dotenv import load_dotenv
from outpost_sdk import Outpost, models


def run():
    load_dotenv()

    server_url = os.environ.get("SERVER_URL", "http://localhost:3333")
    admin_api_key = os.environ.get("ADMIN_API_KEY")
    tenant_id = os.environ.get("TENANT_ID", "hookdeck")

    if not admin_api_key:
        raise Exception("ADMIN_API_KEY not set")

    client = Outpost(
        server_url=f"{server_url}/api/v1",
        security=models.Security(
            admin_api_key=admin_api_key,
        ),
    )

    topic = "order.created"
    payload = {
        "order_id": "ord_2Ua9d1o2b3c4d5e6f7g8h9i0j",
        "customer_id": "cus_1a2b3c4d5e6f7g8h9i0j",
        "total_amount": "99.99",
        "currency": "USD",
        "items": [
            {
                "product_id": "prod_1a2b3c4d5e6f7g8h9i0j",
                "name": "Example Product 1",
                "quantity": 1,
                "price": "49.99",
            },
            {
                "product_id": "prod_9z8y7x6w5v4u3t2s1r0q",
                "name": "Example Product 2",
                "quantity": 1,
                "price": "50.00",
            },
        ],
    }

    res = client.publish.event(
        topic=topic,
        data=payload,
        tenant_id=tenant_id,
    )

    if res is not None:
        print("Event published successfully")
        print(f"Event ID: {res.id}")
    else:
        print("Failed to publish event")
