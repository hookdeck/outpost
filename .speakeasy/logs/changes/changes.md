## Go SDK Changes:
* `Outpost.Attempts.List()`: `error` **Changed** (Breaking ⚠️)
    - `Status[422]` **Removed** (Breaking ⚠️)
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Destinations.ListAttempts()`: 
  *  `request.Request.TenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `Status[401]` **Added**
    - `Status[422]` **Removed** (Breaking ⚠️)
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Publish.Event()`: `error` **Changed** (Breaking ⚠️)
    - `Status[400]` **Removed** (Breaking ⚠️)
    - `` **Added**
* `Outpost.Destinations.GetAttempt()`: 
  *  `request.TenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[401]` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Tenants.ListTenants()`: 
  * `request` **Changed** (Breaking ⚠️)
    - `Dir` **Added**
    - `Limit` **Added**
    - `Next` **Added**
    - `Prev` **Added**
    - `Request` **Removed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `AdditionalProperties` **Added**
    - `Error` **Removed** (Breaking ⚠️)
    - `HttpMeta` **Removed** (Breaking ⚠️)
    - `Message` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Tenants.Upsert()`: 
  *  `request.TenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[401]` **Added**
    - `Status[422]` **Added**
    - `Status[500]` **Added**
* `Outpost.Tenants.Get()`: 
  *  `request.TenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[401]` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Tenants.Delete()`: 
  *  `request.TenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[401]` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Tenants.GetPortalUrl()`: 
  *  `request.TenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[401]` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Tenants.GetToken()`: 
  *  `request.TenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[401]` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Events.List()`: 
  *  `response.Models[].SuccessfulAt` **Removed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `Status[422]` **Removed** (Breaking ⚠️)
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Events.Get()`: 
  *  `response.SuccessfulAt` **Removed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[401]` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Destinations.Disable()`: 
  *  `request.TenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[401]` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Destinations.Enable()`: 
  *  `request.TenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[401]` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Destinations.Update()`: 
  *  `request.TenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `Status[400]` **Removed** (Breaking ⚠️)
    - `Status[401]` **Added**
    - `Status[422]` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Destinations.List()`: 
  *  `request.TenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[401]` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Destinations.Create()`: 
  *  `request.TenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `Status[400]` **Removed** (Breaking ⚠️)
    - `Status[401]` **Added**
    - `Status[422]` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Destinations.Get()`: 
  *  `request.TenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[401]` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Attempts.Retry()`: `error` **Changed** (Breaking ⚠️)
    - `Status[401]` **Added**
    - `Status[422]` **Removed** (Breaking ⚠️)
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Destinations.Delete()`: 
  *  `request.TenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[401]` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Schemas.ListDestinationTypes()`: **Added**
* `Outpost.Schemas.GetDestinationType()`: **Added**
* `Outpost.Attempts.Get()`: `error` **Changed**
    - `Status[401]` **Added**
    - `Status[500]` **Added**
    - `` **Added**
* `Outpost.Schemas.GetDestinationTypeJwt()`: **Removed** (Breaking ⚠️)
* `Outpost.Schemas.ListDestinationTypesJwt()`: **Removed** (Breaking ⚠️)
* `Outpost.Topics.List()`:  `error.status[401]` **Added**
