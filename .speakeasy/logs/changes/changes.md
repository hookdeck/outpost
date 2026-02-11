## Typescript SDK Changes:
* `outpost.tenants.listTenants()`: 
  * `request` **Changed** (Breaking ⚠️)
    - `createdAt[gte]` **Added**
    - `createdAt[lte]` **Added**
    - `dir` **Added**
    - `orderBy` **Added**
    - `order` **Removed** (Breaking ⚠️)
  * `response` **Changed** (Breaking ⚠️)
    - `data` **Removed** (Breaking ⚠️)
    - `models` **Added**
    - `next` **Removed** (Breaking ⚠️)
    - `pagination` **Added**
    - `prev` **Removed** (Breaking ⚠️)
* `outpost.events.get()`: 
  *  `request.tenantId` **Removed** (Breaking ⚠️)
  * `response` **Changed** (Breaking ⚠️)
    - `status` **Removed** (Breaking ⚠️)
    - `tenantId` **Added**
* `outpost.events.list()`: 
  * `request` **Changed** (Breaking ⚠️)
    - `destinationId` **Removed** (Breaking ⚠️)
    - `dir` **Added**
    - `end` **Removed** (Breaking ⚠️)
    - `orderBy` **Added**
    - `start` **Removed** (Breaking ⚠️)
    - `status` **Removed** (Breaking ⚠️)
    - `tenantId` **Changed**
    - `time[gte]` **Added**
    - `time[lte]` **Added**
    - `topic` **Added**
  * `response` **Changed** (Breaking ⚠️)
    - `count` **Removed** (Breaking ⚠️)
    - `data` **Removed** (Breaking ⚠️)
    - `models` **Added**
    - `next` **Removed** (Breaking ⚠️)
    - `pagination` **Added**
    - `prev` **Removed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `status[401]` **Added**
    - `status[404]` **Removed** (Breaking ⚠️)
    - `status[422]` **Added**
* `outpost.topics.list()`: 
  *  `request` **Removed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[400]` **Added**
    - `status[401]` **Added**
    - `status[403]` **Added**
    - `status[407]` **Added**
    - `status[408]` **Added**
    - `status[413]` **Added**
    - `status[414]` **Added**
    - `status[415]` **Added**
    - `status[422]` **Added**
    - `status[429]` **Added**
    - `status[431]` **Added**
    - `status[500]` **Added**
    - `status[501]` **Added**
    - `status[502]` **Added**
    - `status[503]` **Added**
    - `status[504]` **Added**
    - `status[505]` **Added**
    - `status[506]` **Added**
    - `status[507]` **Added**
    - `status[508]` **Added**
    - `status[510]` **Added**
    - `status[511]` **Added**
* `outpost.destinations.getAttempt()`: **Added**
* `outpost.schemas.listTenantDestinationTypes()`: **Removed** (Breaking ⚠️)
* `outpost.schemas.get()`: **Removed** (Breaking ⚠️)
* `outpost.topics.listJwt()`: **Removed** (Breaking ⚠️)
* `outpost.events.listDeliveries()`: **Removed** (Breaking ⚠️)
* `outpost.events.listByDestination()`: **Removed** (Breaking ⚠️)
* `outpost.events.getByDestination()`: **Removed** (Breaking ⚠️)
* `outpost.events.retry()`: **Removed** (Breaking ⚠️)
* `outpost.attempts.list()`: **Added**
* `outpost.publish.event()`: 
  *  `request.time` **Added**
  *  `response.destinationIds` **Added**
* `outpost.destinations.listAttempts()`: **Added**
* `outpost.attempts.retry()`: **Added**
* `outpost.attempts.get()`: **Added**
