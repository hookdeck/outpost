## Go SDK Changes:
* `Outpost.Attempts.List()`: `response.Models[]` **Changed** (Breaking ⚠️)
    - `Destination` **Added**
    - `Event` **Changed** (Breaking ⚠️)
    - `ResponseData` **Changed** (Breaking ⚠️)
* `Outpost.Attempts.Get()`: `response` **Changed** (Breaking ⚠️)
    - `Destination` **Added**
    - `Event` **Changed** (Breaking ⚠️)
    - `ResponseData` **Changed** (Breaking ⚠️)
* `Outpost.Destinations.ListAttempts()`: `response.Models[]` **Changed** (Breaking ⚠️)
    - `Destination` **Added**
    - `Event` **Changed** (Breaking ⚠️)
    - `ResponseData` **Changed** (Breaking ⚠️)
* `Outpost.Destinations.GetAttempt()`: `response` **Changed** (Breaking ⚠️)
    - `Destination` **Added**
    - `Event` **Changed** (Breaking ⚠️)
    - `ResponseData` **Changed** (Breaking ⚠️)
* `Outpost.Schemas.ListDestinationTypes()`: `response.[].ConfigFields[]` **Changed** (Breaking ⚠️)
    - `Options` **Added**
    - `Type.Enum(select)` **Added** (Breaking ⚠️)
* `Outpost.Schemas.GetDestinationType()`: 
  * `request.Type` **Changed**
    - `Enum(kafka)` **Added**
  * `response.ConfigFields[]` **Changed** (Breaking ⚠️)
    - `Options` **Added**
    - `Type.Enum(select)` **Added** (Breaking ⚠️)
* `Outpost.Metrics.GetEventMetrics()`: **Added**
* `Outpost.Metrics.GetAttemptMetrics()`: **Added**
