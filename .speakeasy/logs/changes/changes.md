## Typescript SDK Changes:
* `outpost.events.list()`: 
  *  `request.destinationId` **Added**
  * `response.models[]` **Changed** (Breaking ⚠️)
    - `destinationId` **Removed** (Breaking ⚠️)
    - `matchedDestinationIds` **Added**
* `outpost.events.get()`: `response` **Changed** (Breaking ⚠️)
    - `destinationId` **Removed** (Breaking ⚠️)
    - `matchedDestinationIds` **Added**
* `outpost.schemas.listDestinationTypes()`: `response.[].configFields[]` **Changed** (Breaking ⚠️)
    - `key` **Added**
    - `options` **Added**
    - `type.enum(select)` **Added** (Breaking ⚠️)
* `outpost.schemas.getDestinationType()`: 
  * `request.type` **Changed**
    - `enum(kafka)` **Added**
  * `response.configFields[]` **Changed** (Breaking ⚠️)
    - `key` **Added**
    - `options` **Added**
    - `type.enum(select)` **Added** (Breaking ⚠️)
