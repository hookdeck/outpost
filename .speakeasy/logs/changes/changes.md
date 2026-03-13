## Typescript SDK Changes:
* `outpost.tenants.listTenants()`: `request` **Changed** (Breaking ⚠️)
    - `dir` **Removed** (Breaking ⚠️)
    - `limit` **Removed** (Breaking ⚠️)
    - `next` **Removed** (Breaking ⚠️)
    - `prev` **Removed** (Breaking ⚠️)
    - `request` **Added** (Breaking ⚠️)
* `outpost.events.list()`: `request` **Changed** (Breaking ⚠️)
    - `id` **Added**
    - `tenantId` **Changed** (Breaking ⚠️)
    - `time[gt]` **Added**
    - `time[lt]` **Added**
* `outpost.attempts.list()`: `request` **Changed** (Breaking ⚠️)
    - `destinationId` **Changed** (Breaking ⚠️)
    - `eventId` **Changed** (Breaking ⚠️)
    - `tenantId` **Changed** (Breaking ⚠️)
    - `time[gt]` **Added**
    - `time[lt]` **Added**
* `outpost.destinations.listAttempts()`: `request` **Changed** (Breaking ⚠️)
    - `eventId` **Changed** (Breaking ⚠️)
    - `time[gt]` **Added**
    - `time[lt]` **Added**
