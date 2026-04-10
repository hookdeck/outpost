## Python SDK Changes:
* `outpost.events.list()`: 
  *  `request.destination_id` **Added**
  * `response.models[]` **Changed** (Breaking ⚠️)
    - `destination_id` **Removed** (Breaking ⚠️)
    - `matched_destination_ids` **Added**
* `outpost.events.get()`: `response` **Changed** (Breaking ⚠️)
    - `destination_id` **Removed** (Breaking ⚠️)
    - `matched_destination_ids` **Added**
* `outpost.schemas.list_destination_types()`: `response.[].config_fields[]` **Changed** (Breaking ⚠️)
    - `key` **Added**
    - `options` **Added**
    - `type.enum(select)` **Added** (Breaking ⚠️)
* `outpost.schemas.get_destination_type()`: 
  * `request.type` **Changed**
    - `enum(kafka)` **Added**
  * `response.config_fields[]` **Changed** (Breaking ⚠️)
    - `key` **Added**
    - `options` **Added**
    - `type.enum(select)` **Added** (Breaking ⚠️)
