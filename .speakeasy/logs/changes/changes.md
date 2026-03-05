## Python SDK Changes:
* `outpost.attempts.list()`: `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `status[422]` **Removed** (Breaking ⚠️)
    - `status[500]` **Added**
* `outpost.destinations.list_attempts()`: 
  *  `request.tenant_id` **Changed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `status[401]` **Added**
    - `status[422]` **Removed** (Breaking ⚠️)
    - `status[500]` **Added**
* `outpost.publish.event()`: `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `status[400]` **Removed** (Breaking ⚠️)
* `outpost.destinations.get_attempt()`: 
  *  `request.tenant_id` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.tenants.list_tenants()`: 
  * `request` **Changed** (Breaking ⚠️)
    - `created_at[gte]` **Removed** (Breaking ⚠️)
    - `created_at[lte]` **Removed** (Breaking ⚠️)
    - `dir` **Added**
    - `direction` **Changed** (Breaking ⚠️)
    - `next_cursor` **Removed** (Breaking ⚠️)
    - `order_by` **Removed** (Breaking ⚠️)
    - `prev_cursor` **Removed** (Breaking ⚠️)
    - `prev` **Added**
  * `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `additional_properties` **Added**
    - `error` **Removed** (Breaking ⚠️)
    - `message` **Added**
    - `status[500]` **Added**
* `outpost.tenants.upsert()`: 
  * `request` **Changed** (Breaking ⚠️)
    - `metadata` **Added**
    - `params` **Removed** (Breaking ⚠️)
    - `tenant_id` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `status[401]` **Added**
    - `status[422]` **Added**
    - `status[500]` **Added**
* `outpost.tenants.get()`: 
  *  `request.tenant_id` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.tenants.delete()`: 
  *  `request.tenant_id` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.tenants.get_portal_url()`: 
  *  `request.tenant_id` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.tenants.get_token()`: 
  *  `request.tenant_id` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.events.list()`: 
  *  `response.models[].successful_at` **Removed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `status[422]` **Removed** (Breaking ⚠️)
    - `status[500]` **Added**
* `outpost.events.get()`: 
  *  `response.successful_at` **Removed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.destinations.disable()`: 
  *  `request.tenant_id` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.destinations.enable()`: 
  *  `request.tenant_id` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.destinations.update()`: 
  *  `request.tenant_id` **Changed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `status[400]` **Removed** (Breaking ⚠️)
    - `status[401]` **Added**
    - `status[422]` **Added**
    - `status[500]` **Added**
* `outpost.destinations.list()`: 
  *  `request.tenant_id` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.destinations.create()`: 
  *  `request.tenant_id` **Changed** (Breaking ⚠️)
  * `error` **Changed** (Breaking ⚠️)
    - `` **Added**
    - `status[400]` **Removed** (Breaking ⚠️)
    - `status[401]` **Added**
    - `status[422]` **Added**
    - `status[500]` **Added**
* `outpost.destinations.get()`: 
  *  `request.tenant_id` **Changed** (Breaking ⚠️)
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
  *  `request.tenant_id` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.schemas.list_destination_types()`: **Added**
* `outpost.schemas.get_destination_type()`: **Added**
* `outpost.attempts.get()`: `error` **Changed**
    - `` **Added**
    - `status[401]` **Added**
    - `status[500]` **Added**
* `outpost.schemas.get_destination_type_jwt()`: **Removed** (Breaking ⚠️)
* `outpost.schemas.list_destination_types_jwt()`: **Removed** (Breaking ⚠️)
* `outpost.topics.list()`:  `error.status[401]` **Added**
