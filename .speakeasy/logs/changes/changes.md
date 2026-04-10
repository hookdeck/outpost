## Go SDK Changes:
* `Outpost.Events.List()`: 
  *  `request.Request.DestinationId` **Added**
  * `response.Models[]` **Changed** (Breaking ⚠️)
    - `DestinationId` **Removed** (Breaking ⚠️)
    - `MatchedDestinationIds` **Added**
* `Outpost.Events.Get()`: `response` **Changed** (Breaking ⚠️)
    - `DestinationId` **Removed** (Breaking ⚠️)
    - `MatchedDestinationIds` **Added**
* `Outpost.Schemas.ListDestinationTypes()`: `response.[].ConfigFields[]` **Changed** (Breaking ⚠️)
    - `Key` **Added**
    - `Options` **Added**
    - `Type.Enum(select)` **Added** (Breaking ⚠️)
* `Outpost.Schemas.GetDestinationType()`: 
  * `request.Type` **Changed**
    - `Enum(kafka)` **Added**
  * `response.ConfigFields[]` **Changed** (Breaking ⚠️)
    - `Key` **Added**
    - `Options` **Added**
    - `Type.Enum(select)` **Added** (Breaking ⚠️)
