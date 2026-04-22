## Typescript SDK Changes:
* `outpost.schemas.listDestinationTypes()`: `response.[]` **Changed** (Breaking ⚠️)
    - `remoteSetupUrl` **Removed** (Breaking ⚠️)
    - `setupLink` **Added**
* `outpost.schemas.getDestinationType()`: `response` **Changed** (Breaking ⚠️)
    - `remoteSetupUrl` **Removed** (Breaking ⚠️)
    - `setupLink` **Added**
* `outpost.metrics.getAttemptMetrics()`: `request` **Changed** (Breaking ⚠️)
    - `dimensions.union("tenant_id","destination_id" and 5 more)` **Removed** (Breaking ⚠️)
    - `dimensions.union("tenant_id","destination_id" and 6 more)` **Added**
    - `dimensions.union(Array<"tenant_id","destination_id" and 5 more>)` **Removed** (Breaking ⚠️)
    - `dimensions.union(Array<"tenant_id","destination_id" and 6 more>)` **Added**
    - `filters[destinationType]` **Added**
* `outpost.configuration.getManagedConfig()`: **Added**
* `outpost.configuration.updateManagedConfig()`: **Added**
* `outpost.events.get()`:  `request.tenantId` **Added**
* `outpost.attempts.list()`: 
  *  `request.destinationType` **Added**
* `outpost.attempts.get()`:  `request.tenantId` **Added**
