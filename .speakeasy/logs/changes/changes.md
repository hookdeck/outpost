## Python SDK Changes:
* `outpost.health.check()`: `response` **Changed** (Breaking ⚠️)
    - `status.enum(degraded)` **Added** (Breaking ⚠️)
    - `workers.Map<workers>.reason` **Added**
    - `workers.Map<workers>.since` **Added**
    - `workers.Map<workers>.status.enum(degraded)` **Added** (Breaking ⚠️)
* `outpost.destinations.get_attempt()`: 
  *  `response.destination` **Changed** (Breaking ⚠️)
  *  `error.status[403]` **Added**
* `outpost.destinations.list_attempts()`: 
  *  `response.models[].destination` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `status[400]` **Added**
    - `status[403]` **Added**
    - `status[422]` **Added**
* `outpost.destinations.disable()`: 
  * `response` **Changed** (Breaking ⚠️)
    - `union(aws_eventbridge)` **Added** (Breaking ⚠️)
    - `union(cloudflare_queues)` **Added** (Breaking ⚠️)
  *  `error.status[403]` **Added**
* `outpost.destinations.enable()`: 
  * `response` **Changed** (Breaking ⚠️)
    - `union(aws_eventbridge)` **Added** (Breaking ⚠️)
    - `union(cloudflare_queues)` **Added** (Breaking ⚠️)
  *  `error.status[403]` **Added**
* `outpost.destinations.update()`: 
  * `request.body` **Changed**
    - `union(aws_eventbridge)` **Added**
    - `union(cloudflare_queues)` **Added**
  * `response.union(Destination)` **Changed** (Breaking ⚠️)
    - `union(aws_eventbridge)` **Added** (Breaking ⚠️)
    - `union(cloudflare_queues)` **Added** (Breaking ⚠️)
  *  `error.status[403]` **Added**
* `outpost.destinations.get()`: 
  * `response` **Changed** (Breaking ⚠️)
    - `union(aws_eventbridge)` **Added** (Breaking ⚠️)
    - `union(cloudflare_queues)` **Added** (Breaking ⚠️)
  *  `error.status[403]` **Added**
* `outpost.destinations.create()`: 
  * `request.body` **Changed**
    - `union(aws_eventbridge)` **Added**
    - `union(cloudflare_queues)` **Added**
  * `response` **Changed** (Breaking ⚠️)
    - `union(aws_eventbridge)` **Added** (Breaking ⚠️)
    - `union(cloudflare_queues)` **Added** (Breaking ⚠️)
  * `error` **Changed**
    - `status[400]` **Added**
    - `status[403]` **Added**
* `outpost.destinations.list()`: 
  * `request.type` **Changed**
    - `union(Array<DestinationType>)[].enum(aws_eventbridge)` **Added**
    - `union(Array<DestinationType>)[].enum(cloudflare_queues)` **Added**
    - `union(Array<DestinationType>)[].enum(kafka)` **Added**
  * `response.[]` **Changed** (Breaking ⚠️)
    - `union(aws_eventbridge)` **Added** (Breaking ⚠️)
    - `union(cloudflare_queues)` **Added** (Breaking ⚠️)
  *  `error.status[403]` **Added**
* `outpost.attempts.get()`:  `response.destination` **Changed** (Breaking ⚠️)
* `outpost.attempts.list()`: 
  * `request.destination_type` **Changed**
    - `union(DestinationType).enum(aws_eventbridge)` **Added**
    - `union(DestinationType).enum(cloudflare_queues)` **Added**
    - `union(DestinationType).enum(kafka)` **Added**
  *  `response.models[].destination` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `status[400]` **Added**
    - `status[403]` **Added**
    - `status[422]` **Added**
* `outpost.operator_events.get_event()`: **Added**
* `outpost.events.get()`:  `response.eligible_for_retry` **Added**
* `outpost.operator_events.retry()`: **Added**
* `outpost.publish()`:  `error.status[409]` **Added**
* `outpost.retry()`:  `error.status[422]` **Added**
* `outpost.operator_events.list_attempts()`: **Added**
* `outpost.tenants.list()`:  `error.status[422]` **Added**
* `outpost.tenants.upsert()`:  `error.status[403]` **Added**
* `outpost.tenants.get()`:  `error.status[403]` **Added**
* `outpost.tenants.delete()`:  `error.status[403]` **Added**
* `outpost.tenants.get_portal_url()`:  `error.status[403]` **Added**
* `outpost.tenants.get_token()`:  `error.status[403]` **Added**
* `outpost.events.list()`: 
  *  `response.models[].eligible_for_retry` **Added**
  * `error` **Changed**
    - `status[400]` **Added**
    - `status[403]` **Added**
    - `status[422]` **Added**
* `outpost.operator_events.get_attempt()`: **Added**
* `outpost.operator_events.list_event_attempts()`: **Added**
* `outpost.operator_events.list_destination_types()`: **Added**
* `outpost.operator_events.list_events()`: **Added**
* `outpost.operator_events.disable_destination()`: **Added**
* `outpost.operator_events.enable_destination()`: **Added**
* `outpost.operator_events.delete_destination()`: **Added**
* `outpost.destinations.delete()`:  `error.status[403]` **Added**
* `outpost.operator_events.update_destination()`: **Added**
* `outpost.operator_events.get_destination()`: **Added**
* `outpost.operator_events.create_destination()`: **Added**
* `outpost.operator_events.list_destinations()`: **Added**
* `outpost.schemas.get_destination_type()`: `request.type` **Changed**
    - `enum(aws_eventbridge)` **Added**
    - `enum(cloudflare_queues)` **Added**
* `outpost.metrics.get_attempt_metrics()`: 
  * `request.filters[destination_type]` **Changed**
    - `union(Array<DestinationType>)[].enum(aws_eventbridge)` **Added**
    - `union(Array<DestinationType>)[].enum(cloudflare_queues)` **Added**
    - `union(Array<DestinationType>)[].enum(kafka)` **Added**
