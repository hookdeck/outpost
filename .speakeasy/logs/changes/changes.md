## Go SDK Changes:
* `Outpost.Tenants.ListTenants()`: 
  * `request` **Changed** (Breaking ⚠️)
    - `Limit` **Removed** (Breaking ⚠️)
    - `Next` **Removed** (Breaking ⚠️)
    - `Order` **Removed** (Breaking ⚠️)
    - `Prev` **Removed** (Breaking ⚠️)
    - `Request` **Added** (Breaking ⚠️)
  * `response` **Changed** (Breaking ⚠️)
    - `Data` **Removed** (Breaking ⚠️)
    - `Models` **Added**
    - `Next` **Removed** (Breaking ⚠️)
    - `Pagination` **Added**
    - `Prev` **Removed** (Breaking ⚠️)
* `Outpost.Events.Get()`: 
  *  `request.TenantId` **Removed** (Breaking ⚠️)
  * `response` **Changed** (Breaking ⚠️)
    - `Status` **Removed** (Breaking ⚠️)
    - `TenantId` **Added**
* `Outpost.Events.List()`: 
  * `request.Request` **Changed** (Breaking ⚠️)
    - `DestinationId` **Removed** (Breaking ⚠️)
    - `Dir` **Added**
    - `End` **Removed** (Breaking ⚠️)
    - `OrderBy` **Added**
    - `Start` **Removed** (Breaking ⚠️)
    - `Status` **Removed** (Breaking ⚠️)
    - `TenantId` **Changed**
    - `Time[gte]` **Added**
    - `Time[lte]` **Added**
    - `Topic` **Added**
  * `response` **Changed** (Breaking ⚠️)
    - `Count` **Removed** (Breaking ⚠️)
    - `Data` **Removed** (Breaking ⚠️)
    - `Models` **Added**
    - `Next` **Removed** (Breaking ⚠️)
    - `Pagination` **Added**
    - `Prev` **Removed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `Status[401]` **Added**
    - `Status[404]` **Removed** (Breaking ⚠️)
    - `Status[422]` **Added**
* `Outpost.Topics.List()`: 
  *  `request.TenantId` **Removed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[400]` **Added**
    - `Status[401]` **Added**
    - `Status[403]` **Added**
    - `Status[407]` **Added**
    - `Status[408]` **Added**
    - `Status[413]` **Added**
    - `Status[414]` **Added**
    - `Status[415]` **Added**
    - `Status[422]` **Added**
    - `Status[429]` **Added**
    - `Status[431]` **Added**
    - `Status[500]` **Added**
    - `Status[501]` **Added**
    - `Status[502]` **Added**
    - `Status[503]` **Added**
    - `Status[504]` **Added**
    - `Status[505]` **Added**
    - `Status[506]` **Added**
    - `Status[507]` **Added**
    - `Status[508]` **Added**
    - `Status[510]` **Added**
    - `Status[511]` **Added**
    - `` **Added**
* `Outpost.Destinations.GetAttempt()`: **Added**
* `Outpost.Schemas.ListTenantDestinationTypes()`: **Removed** (Breaking ⚠️)
* `Outpost.Schemas.Get()`: **Removed** (Breaking ⚠️)
* `Outpost.Topics.ListJwt()`: **Removed** (Breaking ⚠️)
* `Outpost.Events.ListDeliveries()`: **Removed** (Breaking ⚠️)
* `Outpost.Events.ListByDestination()`: **Removed** (Breaking ⚠️)
* `Outpost.Events.GetByDestination()`: **Removed** (Breaking ⚠️)
* `Outpost.Events.Retry()`: **Removed** (Breaking ⚠️)
* `Outpost.Attempts.List()`: **Added**
* `Outpost.Publish.Event()`: 
  *  `request.Request.Time` **Added**
  *  `response.DestinationIds` **Added**
* `Outpost.Destinations.ListAttempts()`: **Added**
* `Outpost.Attempts.Retry()`: **Added**
* `Outpost.Attempts.Get()`: **Added**
