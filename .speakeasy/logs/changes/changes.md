## Python SDK Changes:
* `outpost.configuration.get_managed_config()`: `response` **Changed** (Breaking ⚠️)
    - `alert_callback_url` **Removed** (Breaking ⚠️)
    - `alert_exhausted_retries_window_seconds` **Added**
    - `organization_name` **Removed** (Breaking ⚠️)
* `outpost.configuration.update_managed_config()`: 
  * `request` **Changed** (Breaking ⚠️)
    - `alert_callback_url` **Removed** (Breaking ⚠️)
    - `alert_exhausted_retries_window_seconds` **Added**
    - `organization_name` **Removed** (Breaking ⚠️)
  * `response` **Changed** (Breaking ⚠️)
    - `alert_callback_url` **Removed** (Breaking ⚠️)
    - `alert_exhausted_retries_window_seconds` **Added**
    - `organization_name` **Removed** (Breaking ⚠️)
