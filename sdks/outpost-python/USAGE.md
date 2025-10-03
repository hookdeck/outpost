<!-- Start SDK Example Usage [usage] -->
```python
# Synchronous Example
from outpost_sdk import Outpost


with Outpost() as outpost:

    res = outpost.health.check()

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

    async with Outpost() as outpost:

        res = await outpost.health.check_async()

        # Handle response
        print(res)

asyncio.run(main())
```
<!-- End SDK Example Usage [usage] -->