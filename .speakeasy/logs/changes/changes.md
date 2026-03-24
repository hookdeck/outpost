## Typescript SDK Changes:
* `outpost.attempts.list()`: `response.models[]` **Changed** (Breaking ⚠️)
    - `destination` **Added**
    - `event` **Changed** (Breaking ⚠️)
    - `responseData` **Changed** (Breaking ⚠️)
* `outpost.attempts.get()`: `response` **Changed** (Breaking ⚠️)
    - `destination` **Added**
    - `event` **Changed** (Breaking ⚠️)
    - `responseData` **Changed** (Breaking ⚠️)
* `outpost.destinations.listAttempts()`: `response.models[]` **Changed** (Breaking ⚠️)
    - `destination` **Added**
    - `event` **Changed** (Breaking ⚠️)
    - `responseData` **Changed** (Breaking ⚠️)
* `outpost.destinations.getAttempt()`: `response` **Changed** (Breaking ⚠️)
    - `destination` **Added**
    - `event` **Changed** (Breaking ⚠️)
    - `responseData` **Changed** (Breaking ⚠️)
* `outpost.schemas.listDestinationTypes()`: `response.[].configFields[]` **Changed** (Breaking ⚠️)
    - `options` **Added**
    - `type.enum(select)` **Added** (Breaking ⚠️)
* `outpost.schemas.getDestinationType()`: 
  * `request.type` **Changed**
    - `enum(kafka)` **Added**
  * `response.configFields[]` **Changed** (Breaking ⚠️)
    - `options` **Added**
    - `type.enum(select)` **Added** (Breaking ⚠️)
* `outpost.metrics.getEventMetrics()`: **Added**
* `outpost.metrics.getAttemptMetrics()`: **Added**
