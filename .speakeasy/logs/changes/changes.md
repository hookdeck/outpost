## Typescript SDK Changes:
* `outpost.health.check()`: `response` **Changed** (Breaking ⚠️)
    - `status.enum(degraded)` **Added** (Breaking ⚠️)
    - `workers.Map<workers>.reason` **Added**
    - `workers.Map<workers>.since` **Added**
    - `workers.Map<workers>.status.enum(degraded)` **Added** (Breaking ⚠️)
* `outpost.destinations.getAttempt()`: 
  *  `response.destination` **Changed** (Breaking ⚠️)
  *  `error.status[403]` **Added**
* `outpost.destinations.listAttempts()`: 
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
    - `union(DestinationType).enum(awsEventbridge)` **Added**
    - `union(DestinationType).enum(cloudflareQueues)` **Added**
    - `union(DestinationType).enum(kafka)` **Added**
  * `response.[]` **Changed** (Breaking ⚠️)
    - `union(aws_eventbridge)` **Added** (Breaking ⚠️)
    - `union(cloudflare_queues)` **Added** (Breaking ⚠️)
  *  `error.status[403]` **Added**
* `outpost.attempts.get()`:  `response.destination` **Changed** (Breaking ⚠️)
* `outpost.attempts.list()`: 
  * `request.destinationType` **Changed**
    - `union(DestinationType).enum(awsEventbridge)` **Added**
    - `union(DestinationType).enum(cloudflareQueues)` **Added**
    - `union(DestinationType).enum(kafka)` **Added**
  *  `response.models[].destination` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `status[400]` **Added**
    - `status[403]` **Added**
    - `status[422]` **Added**
* `outpost.operatorEvents.getEvent()`: **Added**
* `outpost.events.get()`:  `response.eligibleForRetry` **Added**
* `outpost.operatorEvents.retry()`: **Added**
* `outpost.publish()`:  `error.status[409]` **Added**
* `outpost.retry()`:  `error.status[422]` **Added**
* `outpost.operatorEvents.listAttempts()`: **Added**
* `outpost.tenants.list()`:  `error.status[422]` **Added**
* `outpost.tenants.upsert()`:  `error.status[403]` **Added**
* `outpost.tenants.get()`:  `error.status[403]` **Added**
* `outpost.tenants.delete()`:  `error.status[403]` **Added**
* `outpost.tenants.getPortalUrl()`:  `error.status[403]` **Added**
* `outpost.tenants.getToken()`:  `error.status[403]` **Added**
* `outpost.events.list()`: 
  *  `response.models[].eligibleForRetry` **Added**
  * `error` **Changed**
    - `status[400]` **Added**
    - `status[403]` **Added**
    - `status[422]` **Added**
* `outpost.operatorEvents.getAttempt()`: **Added**
* `outpost.operatorEvents.listEventAttempts()`: **Added**
* `outpost.operatorEvents.listDestinationTypes()`: **Added**
* `outpost.operatorEvents.listEvents()`: **Added**
* `outpost.operatorEvents.disableDestination()`: **Added**
* `outpost.operatorEvents.enableDestination()`: **Added**
* `outpost.operatorEvents.deleteDestination()`: **Added**
* `outpost.destinations.delete()`:  `error.status[403]` **Added**
* `outpost.operatorEvents.updateDestination()`: **Added**
* `outpost.operatorEvents.getDestination()`: **Added**
* `outpost.operatorEvents.createDestination()`: **Added**
* `outpost.operatorEvents.listDestinations()`: **Added**
* `outpost.schemas.getDestinationType()`: `request.type` **Changed**
    - `enum(awsEventbridge)` **Added**
    - `enum(cloudflareQueues)` **Added**
* `outpost.metrics.getAttemptMetrics()`: 
  * `request.filters[destinationType]` **Changed**
    - `union(DestinationType).enum(awsEventbridge)` **Added**
    - `union(DestinationType).enum(cloudflareQueues)` **Added**
    - `union(DestinationType).enum(kafka)` **Added**
