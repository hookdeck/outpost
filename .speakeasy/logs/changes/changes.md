## Go SDK Changes:
* `Outpost.Health.Check()`: `response` **Changed** (Breaking ⚠️)
    - `Status.Enum(degraded)` **Added** (Breaking ⚠️)
    - `Workers.Map<workers>.Reason` **Added**
    - `Workers.Map<workers>.Since` **Added**
    - `Workers.Map<workers>.Status.Enum(degraded)` **Added** (Breaking ⚠️)
* `Outpost.Destinations.GetAttempt()`: 
  *  `response.Destination` **Changed** (Breaking ⚠️)
  *  `error.status[403]` **Added**
* `Outpost.Destinations.ListAttempts()`: 
  *  `response.Models[].Destination` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[400]` **Added**
    - `Status[403]` **Added**
    - `Status[422]` **Added**
* `Outpost.Destinations.Disable()`: 
  * `response` **Changed** (Breaking ⚠️)
    - `union(aws_eventbridge)` **Added** (Breaking ⚠️)
    - `union(cloudflare_queues)` **Added** (Breaking ⚠️)
  *  `error.status[403]` **Added**
* `Outpost.Destinations.Enable()`: 
  * `response` **Changed** (Breaking ⚠️)
    - `union(aws_eventbridge)` **Added** (Breaking ⚠️)
    - `union(cloudflare_queues)` **Added** (Breaking ⚠️)
  *  `error.status[403]` **Added**
* `Outpost.Destinations.Update()`: 
  * `request.Body` **Changed**
    - `union(aws_eventbridge)` **Added**
    - `union(cloudflare_queues)` **Added**
  * `response.union(Destination)` **Changed** (Breaking ⚠️)
    - `union(aws_eventbridge)` **Added** (Breaking ⚠️)
    - `union(cloudflare_queues)` **Added** (Breaking ⚠️)
  *  `error.status[403]` **Added**
* `Outpost.Destinations.Get()`: 
  * `response` **Changed** (Breaking ⚠️)
    - `union(aws_eventbridge)` **Added** (Breaking ⚠️)
    - `union(cloudflare_queues)` **Added** (Breaking ⚠️)
  *  `error.status[403]` **Added**
* `Outpost.Destinations.Create()`: 
  * `request.Body` **Changed**
    - `union(aws_eventbridge)` **Added**
    - `union(cloudflare_queues)` **Added**
  * `response` **Changed** (Breaking ⚠️)
    - `union(aws_eventbridge)` **Added** (Breaking ⚠️)
    - `union(cloudflare_queues)` **Added** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[400]` **Added**
    - `Status[403]` **Added**
* `Outpost.Destinations.List()`: 
  * `request.Type` **Changed**
    - `[].Enum(awsEventbridge)` **Added**
    - `[].Enum(cloudflareQueues)` **Added**
    - `[].Enum(kafka)` **Added**
  * `response.[]` **Changed** (Breaking ⚠️)
    - `union(aws_eventbridge)` **Added** (Breaking ⚠️)
    - `union(cloudflare_queues)` **Added** (Breaking ⚠️)
  *  `error.status[403]` **Added**
* `Outpost.Attempts.Get()`:  `response.Destination` **Changed** (Breaking ⚠️)
* `Outpost.Attempts.List()`: 
  * `request.Request.DestinationType` **Changed**
    - `union(DestinationType).Enum(awsEventbridge)` **Added**
    - `union(DestinationType).Enum(cloudflareQueues)` **Added**
    - `union(DestinationType).Enum(kafka)` **Added**
  *  `response.Models[].Destination` **Changed** (Breaking ⚠️)
  * `error` **Changed**
    - `Status[400]` **Added**
    - `Status[403]` **Added**
    - `Status[422]` **Added**
* `Outpost.OperatorEvents.GetEvent()`: **Added**
* `Outpost.Events.Get()`:  `response.EligibleForRetry` **Added**
* `Outpost.OperatorEvents.Retry()`: **Added**
* `Outpost.Publish()`:  `error.status[409]` **Added**
* `Outpost.Retry()`:  `error.status[422]` **Added**
* `Outpost.OperatorEvents.ListAttempts()`: **Added**
* `Outpost.Tenants.List()`:  `error.status[422]` **Added**
* `Outpost.Tenants.Upsert()`:  `error.status[403]` **Added**
* `Outpost.Tenants.Get()`:  `error.status[403]` **Added**
* `Outpost.Tenants.Delete()`:  `error.status[403]` **Added**
* `Outpost.Tenants.GetPortalUrl()`:  `error.status[403]` **Added**
* `Outpost.Tenants.GetToken()`:  `error.status[403]` **Added**
* `Outpost.Events.List()`: 
  *  `response.Models[].EligibleForRetry` **Added**
  * `error` **Changed**
    - `Status[400]` **Added**
    - `Status[403]` **Added**
    - `Status[422]` **Added**
* `Outpost.OperatorEvents.GetAttempt()`: **Added**
* `Outpost.OperatorEvents.ListEventAttempts()`: **Added**
* `Outpost.OperatorEvents.ListDestinationTypes()`: **Added**
* `Outpost.OperatorEvents.ListEvents()`: **Added**
* `Outpost.OperatorEvents.DisableDestination()`: **Added**
* `Outpost.OperatorEvents.EnableDestination()`: **Added**
* `Outpost.OperatorEvents.DeleteDestination()`: **Added**
* `Outpost.Destinations.Delete()`:  `error.status[403]` **Added**
* `Outpost.OperatorEvents.UpdateDestination()`: **Added**
* `Outpost.OperatorEvents.GetDestination()`: **Added**
* `Outpost.OperatorEvents.CreateDestination()`: **Added**
* `Outpost.OperatorEvents.ListDestinations()`: **Added**
* `Outpost.Schemas.GetDestinationType()`: `request.Type` **Changed**
    - `Enum(awsEventbridge)` **Added**
    - `Enum(cloudflareQueues)` **Added**
* `Outpost.Metrics.GetAttemptMetrics()`: 
  * `request.Request.Filters[destinationType]` **Changed**
    - `union(DestinationType).Enum(awsEventbridge)` **Added**
    - `union(DestinationType).Enum(cloudflareQueues)` **Added**
    - `union(DestinationType).Enum(kafka)` **Added**
