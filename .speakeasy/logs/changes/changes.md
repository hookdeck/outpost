## Python SDK Changes:
* `outpost.schemas.list_destination_types()`: `response.[]` **Changed** (Breaking ⚠️)
    - `remote_setup_url` **Removed** (Breaking ⚠️)
    - `setup_link` **Added**
* `outpost.schemas.get_destination_type()`: `response` **Changed** (Breaking ⚠️)
    - `remote_setup_url` **Removed** (Breaking ⚠️)
    - `setup_link` **Added**
* `outpost.metrics.get_attempt_metrics()`: `request` **Changed** (Breaking ⚠️)
    - `dimensions.union("tenant_id","destination_id" and 5 more)` **Removed** (Breaking ⚠️)
    - `dimensions.union("tenant_id","destination_id" and 6 more)` **Added**
    - `dimensions.union(Array<"tenant_id","destination_id" and 5 more>)` **Removed** (Breaking ⚠️)
    - `dimensions.union(Array<"tenant_id","destination_id" and 6 more>)` **Added**
    - `filters[destination_type]` **Added**
* `outpost.configuration.get_managed_config()`: **Added**
* `outpost.configuration.update_managed_config()`: **Added**
* `outpost.events.get()`:  `request.tenant_id` **Added**
* `outpost.attempts.list()`: 
  *  `request.destination_type` **Added**
* `outpost.attempts.get()`:  `request.tenant_id` **Added**
