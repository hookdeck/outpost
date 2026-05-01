## Typescript SDK Changes:
* `outpost.destinations.create()`: 
  *  `request.body.union(kafka)` **Added**
  *  `response.union(kafka)` **Added** (Breaking ⚠️)
* `outpost.destinations.disable()`:  `response.union(kafka)` **Added** (Breaking ⚠️)
* `outpost.events.list()`: `request` **Changed** (Breaking ⚠️)
    - `time[gt]` **Removed** (Breaking ⚠️)
    - `time[gte]` **Removed** (Breaking ⚠️)
    - `time[lt]` **Removed** (Breaking ⚠️)
    - `time[lte]` **Removed** (Breaking ⚠️)
    - `time` **Added**
* `outpost.attempts.list()`: 
  * `request` **Changed** (Breaking ⚠️)
    - `time[gt]` **Removed** (Breaking ⚠️)
    - `time[gte]` **Removed** (Breaking ⚠️)
    - `time[lt]` **Removed** (Breaking ⚠️)
    - `time[lte]` **Removed** (Breaking ⚠️)
    - `time` **Added**
  *  `response.models[].destination.union(kafka)` **Added** (Breaking ⚠️)
* `outpost.attempts.get()`:  `response.destination.union(kafka)` **Added** (Breaking ⚠️)
* `outpost.destinations.list()`:  `response.[].union(kafka)` **Added** (Breaking ⚠️)
* `outpost.metrics.getAttemptMetrics()`: `request` **Changed** (Breaking ⚠️)
    - `time[end]` **Removed** (Breaking ⚠️)
    - `time[start]` **Removed** (Breaking ⚠️)
    - `time` **Added** (Breaking ⚠️)
* `outpost.destinations.update()`: 
  *  `request.body.union(DestinationUpdateKafka)` **Added**
  *  `response.union(Destination).union(kafka)` **Added** (Breaking ⚠️)
* `outpost.metrics.getEventMetrics()`: `request` **Changed** (Breaking ⚠️)
    - `time[end]` **Removed** (Breaking ⚠️)
    - `time[start]` **Removed** (Breaking ⚠️)
    - `time` **Added** (Breaking ⚠️)
* `outpost.destinations.enable()`:  `response.union(kafka)` **Added** (Breaking ⚠️)
* `outpost.destinations.get()`:  `response.union(kafka)` **Added** (Breaking ⚠️)
* `outpost.destinations.listAttempts()`: 
  * `request` **Changed** (Breaking ⚠️)
    - `time[gt]` **Removed** (Breaking ⚠️)
    - `time[gte]` **Removed** (Breaking ⚠️)
    - `time[lt]` **Removed** (Breaking ⚠️)
    - `time[lte]` **Removed** (Breaking ⚠️)
    - `time` **Added**
  *  `response.models[].destination.union(kafka)` **Added** (Breaking ⚠️)
* `outpost.destinations.getAttempt()`:  `response.destination.union(kafka)` **Added** (Breaking ⚠️)
* `outpost.retry.retry()`: **Added**
* `outpost.attempts.retry()`: **Removed** (Breaking ⚠️)
