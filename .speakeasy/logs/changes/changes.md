## Go SDK Changes:
* `Outpost.Configuration.GetManagedConfig()`: `response` **Changed** (Breaking ⚠️)
    - `AlertCallbackUrl` **Removed** (Breaking ⚠️)
    - `AlertExhaustedRetriesWindowSeconds` **Added**
    - `OrganizationName` **Removed** (Breaking ⚠️)
* `Outpost.Configuration.UpdateManagedConfig()`: 
  * `request.Request` **Changed** (Breaking ⚠️)
    - `AlertCallbackUrl` **Removed** (Breaking ⚠️)
    - `AlertExhaustedRetriesWindowSeconds` **Added**
    - `OrganizationName` **Removed** (Breaking ⚠️)
  * `response` **Changed** (Breaking ⚠️)
    - `AlertCallbackUrl` **Removed** (Breaking ⚠️)
    - `AlertExhaustedRetriesWindowSeconds` **Added**
    - `OrganizationName` **Removed** (Breaking ⚠️)
