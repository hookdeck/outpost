## Typescript SDK Changes:
* `outpost.metrics.getAttemptMetrics()`: 
  * `request.measures` **Changed** (Breaking ⚠️)
    - `union("count","successful_count" and 13 more)` **Added**
    - `union("count","successful_count" and 9 more)` **Removed** (Breaking ⚠️)
    - `union(Array<"count","successful_count" and 13 more>)` **Added**
    - `union(Array<"count","successful_count" and 9 more>)` **Removed** (Breaking ⚠️)
* `outpost.attempts.list()`:  `response.models[].latencyMs` **Added**
* `outpost.attempts.get()`:  `response.latencyMs` **Added**
* `outpost.destinations.listAttempts()`:  `response.models[].latencyMs` **Added**
* `outpost.destinations.getAttempt()`:  `response.latencyMs` **Added**
