## Go SDK Changes:
* `Outpost.Destinations.Create()`: 
  *  `request.Body.union(kafka)` **Added**
  *  `response.union(kafka)` **Added** (Breaking ⚠️)
* `Outpost.Destinations.Disable()`:  `response.union(kafka)` **Added** (Breaking ⚠️)
* `Outpost.Events.List()`: `request.Request` **Changed** (Breaking ⚠️)
    - `Time[gt]` **Removed** (Breaking ⚠️)
    - `Time[gte]` **Removed** (Breaking ⚠️)
    - `Time[lt]` **Removed** (Breaking ⚠️)
    - `Time[lte]` **Removed** (Breaking ⚠️)
    - `Time` **Added**
* `Outpost.Attempts.List()`: 
  * `request.Request` **Changed** (Breaking ⚠️)
    - `Time[gt]` **Removed** (Breaking ⚠️)
    - `Time[gte]` **Removed** (Breaking ⚠️)
    - `Time[lt]` **Removed** (Breaking ⚠️)
    - `Time[lte]` **Removed** (Breaking ⚠️)
    - `Time` **Added**
  *  `response.Models[].Destination.union(kafka)` **Added** (Breaking ⚠️)
* `Outpost.Attempts.Get()`:  `response.Destination.union(kafka)` **Added** (Breaking ⚠️)
* `Outpost.Destinations.List()`:  `response.[].union(kafka)` **Added** (Breaking ⚠️)
* `Outpost.Metrics.GetAttemptMetrics()`: `request.Request` **Changed** (Breaking ⚠️)
    - `Time[end]` **Removed** (Breaking ⚠️)
    - `Time[start]` **Removed** (Breaking ⚠️)
    - `Time` **Added** (Breaking ⚠️)
* `Outpost.Destinations.Update()`: 
  *  `request.Body.union(DestinationUpdateKafka)` **Added**
  *  `response.union(Destination).union(kafka)` **Added** (Breaking ⚠️)
* `Outpost.Metrics.GetEventMetrics()`: `request.Request` **Changed** (Breaking ⚠️)
    - `Time[end]` **Removed** (Breaking ⚠️)
    - `Time[start]` **Removed** (Breaking ⚠️)
    - `Time` **Added** (Breaking ⚠️)
* `Outpost.Destinations.Enable()`:  `response.union(kafka)` **Added** (Breaking ⚠️)
* `Outpost.Destinations.Get()`:  `response.union(kafka)` **Added** (Breaking ⚠️)
* `Outpost.Destinations.ListAttempts()`: 
  * `request.Request` **Changed** (Breaking ⚠️)
    - `Time[gt]` **Removed** (Breaking ⚠️)
    - `Time[gte]` **Removed** (Breaking ⚠️)
    - `Time[lt]` **Removed** (Breaking ⚠️)
    - `Time[lte]` **Removed** (Breaking ⚠️)
    - `Time` **Added**
  *  `response.Models[].Destination.union(kafka)` **Added** (Breaking ⚠️)
* `Outpost.Destinations.GetAttempt()`:  `response.Destination.union(kafka)` **Added** (Breaking ⚠️)
* `Outpost.Retry.Retry()`: **Added**
* `Outpost.Attempts.Retry()`: **Removed** (Breaking ⚠️)
