## Typescript SDK Changes:
* `outpost.attempts.list()`: `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `status[422]` **Removed** (Breaking ⚠️)
    - `status[500]` **Added**
* `outpost.destinations.listAttempts()`: 
  *  `request.tenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `status[401]` **Added**
    - `status[422]` **Removed** (Breaking ⚠️)
    - `status[500]` **Added**
* `outpost.publish.event()`: `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `status[400]` **Removed** (Breaking ⚠️)
* `outpost.destinations.getAttempt()`: 
  *  `request.tenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.tenants.listTenants()`: 
  * `request` **Changed** (Breaking ⚠️)
    - `dir` **Added**
    - `limit` **Added**
    - `next` **Added**
    - `prev` **Added**
    - `request` **Removed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `additionalProperties` **Added**
    - `error` **Removed** (Breaking ⚠️)
    - `message` **Added**
    - `status[500]` **Added**
* `outpost.tenants.upsert()`: 
  *  `request.tenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `status[401]` **Added**
    - `status[422]` **Added**
    - `status[500]` **Added**
* `outpost.tenants.get()`: 
  *  `request.tenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.tenants.delete()`: 
  *  `request.tenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.tenants.getPortalUrl()`: 
  *  `request.tenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.tenants.getToken()`: 
  *  `request.tenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.events.list()`: 
  *  `response.models[].successfulAt` **Removed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `status[422]` **Removed** (Breaking ⚠️)
    - `status[500]` **Added**
* `outpost.events.get()`: 
  *  `response.successfulAt` **Removed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.destinations.disable()`: 
  *  `request.tenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.destinations.enable()`: 
  *  `request.tenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.destinations.update()`: 
  *  `request.tenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `status[400]` **Removed** (Breaking ⚠️)
    - `status[401]` **Added**
    - `status[422]` **Added**
    - `status[500]` **Added**
* `outpost.destinations.list()`: 
  *  `request.tenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.destinations.create()`: 
  *  `request.tenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `status[400]` **Removed** (Breaking ⚠️)
    - `status[401]` **Added**
    - `status[422]` **Added**
    - `status[500]` **Added**
* `outpost.destinations.get()`: 
  *  `request.tenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.attempts.retry()`: `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `status[401]` **Added**
    - `status[422]` **Removed** (Breaking ⚠️)
    - `status[500]` **Added**
* `outpost.destinations.delete()`: 
  *  `request.tenantId` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.schemas.listDestinationTypes()`: **Added**
* `outpost.schemas.getDestinationType()`: **Added**
* `outpost.attempts.get()`: `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.schemas.getDestinationTypeJwt()`: **Removed** (Breaking ⚠️)
* `outpost.schemas.listDestinationTypesJwt()`: **Removed** (Breaking ⚠️)
* `outpost.topics.list()`:  `error.status[401]` **Added**
