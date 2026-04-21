## Go SDK Changes:
* `Outpost.Schemas.ListDestinationTypes()`: `response.[]` **Changed** (Breaking ⚠️)
    - `RemoteSetupUrl` **Removed** (Breaking ⚠️)
    - `SetupLink` **Added**
* `Outpost.Schemas.GetDestinationType()`: `response` **Changed** (Breaking ⚠️)
    - `RemoteSetupUrl` **Removed** (Breaking ⚠️)
    - `SetupLink` **Added**
* `Outpost.Metrics.GetAttemptMetrics()`: `request.Request` **Changed** (Breaking ⚠️)
    - `Dimensions.union("tenant_id","destination_id" and 5 more)` **Removed** (Breaking ⚠️)
    - `Dimensions.union("tenant_id","destination_id" and 6 more)` **Added**
    - `Dimensions.union(Array<"tenant_id","destination_id" and 5 more>)` **Removed** (Breaking ⚠️)
    - `Dimensions.union(Array<"tenant_id","destination_id" and 6 more>)` **Added**
    - `Filters[destinationType]` **Added**
* `Outpost.Configuration.GetManagedConfig()`: **Added**
* `Outpost.Configuration.UpdateManagedConfig()`: **Added**
* `Outpost.Events.Get()`:  `request.TenantId` **Added**
* `Outpost.Attempts.List()`: 
  *  `request.Request.DestinationType` **Added**
* `Outpost.Attempts.Get()`:  `request.TenantId` **Added**
