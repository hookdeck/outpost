<!-- Start SDK Example Usage [usage] -->
```python
# Synchronous Example
from outpost_sdk import Outpost


with Outpost(
    api_key="<YOUR_BEARER_TOKEN_HERE>",
) as outpost:

    res = outpost.publish(request={
        "id": "evt_abc123xyz789",
        "tenant_id": "tenant_123",
        "topic": "user.created",
        "eligible_for_retry": True,
        "metadata": {
            "source": "crm",
        },
        "data": {
            "user_id": "userid",
            "status": "active",
        },
    })

    # Handle response
    print(res)
```

</br>

The same SDK client can also be used to make asynchronous requests by importing asyncio.

```python
# Asynchronous Example
import asyncio
from outpost_sdk import Outpost

async def main():

    async with Outpost(
        api_key="<YOUR_BEARER_TOKEN_HERE>",
    ) as outpost:

        res = await outpost.publish_async(request={
            "id": "evt_abc123xyz789",
            "tenant_id": "tenant_123",
            "topic": "user.created",
            "eligible_for_retry": True,
            "metadata": {
                "source": "crm",
            },
            "data": {
                "user_id": "userid",
                "status": "active",
            },
        })

        # Handle response
        print(res)

asyncio.run(main())
```
<!-- End SDK Example Usage [usage] -->