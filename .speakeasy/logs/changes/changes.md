## Go SDK Changes:
* `Outpost.Metrics.GetAttemptMetrics()`: 
  * `request.Request.Measures` **Changed** (Breaking ⚠️)
    - `union("count","successful_count" and 13 more)` **Added**
    - `union("count","successful_count" and 9 more)` **Removed** (Breaking ⚠️)
    - `union(Array<"count","successful_count" and 13 more>)` **Added**
    - `union(Array<"count","successful_count" and 9 more>)` **Removed** (Breaking ⚠️)
* `Outpost.Attempts.List()`:  `response.Models[].LatencyMs` **Added**
* `Outpost.Attempts.Get()`:  `response.LatencyMs` **Added**
* `Outpost.Destinations.ListAttempts()`:  `response.Models[].LatencyMs` **Added**
* `Outpost.Destinations.GetAttempt()`:  `response.LatencyMs` **Added**
