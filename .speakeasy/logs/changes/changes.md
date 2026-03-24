## Python SDK Changes:
* `outpost.attempts.list()`: `response.models[]` **Changed** (Breaking ⚠️)
    - `destination` **Added**
    - `event` **Changed** (Breaking ⚠️)
    - `response_data` **Changed** (Breaking ⚠️)
* `outpost.attempts.get()`: `response` **Changed** (Breaking ⚠️)
    - `destination` **Added**
    - `event` **Changed** (Breaking ⚠️)
    - `response_data` **Changed** (Breaking ⚠️)
* `outpost.destinations.list_attempts()`: `response.models[]` **Changed** (Breaking ⚠️)
    - `destination` **Added**
    - `event` **Changed** (Breaking ⚠️)
    - `response_data` **Changed** (Breaking ⚠️)
* `outpost.destinations.get_attempt()`: `response` **Changed** (Breaking ⚠️)
    - `destination` **Added**
    - `event` **Changed** (Breaking ⚠️)
    - `response_data` **Changed** (Breaking ⚠️)
* `outpost.schemas.list_destination_types()`: `response.[].config_fields[]` **Changed** (Breaking ⚠️)
    - `options` **Added**
    - `type.enum(select)` **Added** (Breaking ⚠️)
* `outpost.schemas.get_destination_type()`: 
  * `request.type` **Changed**
    - `enum(kafka)` **Added**
  * `response.config_fields[]` **Changed** (Breaking ⚠️)
    - `options` **Added**
    - `type.enum(select)` **Added** (Breaking ⚠️)
* `outpost.metrics.get_event_metrics()`: **Added**
* `outpost.metrics.get_attempt_metrics()`: **Added**
