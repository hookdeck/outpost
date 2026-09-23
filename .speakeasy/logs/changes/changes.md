## Python SDK Changes:
* `outpost.metrics.get_attempt_metrics()`: 
  * `request.measures` **Changed** (Breaking ⚠️)
    - `union("count","successful_count" and 13 more)` **Added**
    - `union("count","successful_count" and 9 more)` **Removed** (Breaking ⚠️)
    - `union(Array<"count","successful_count" and 13 more>)` **Added**
    - `union(Array<"count","successful_count" and 9 more>)` **Removed** (Breaking ⚠️)
* `outpost.attempts.list()`:  `response.models[].latency_ms` **Added**
* `outpost.attempts.get()`:  `response.latency_ms` **Added**
* `outpost.destinations.list_attempts()`:  `response.models[].latency_ms` **Added**
* `outpost.destinations.get_attempt()`:  `response.latency_ms` **Added**
