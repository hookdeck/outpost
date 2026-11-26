## Python SDK Changes:
* `outpost.configuration.get_managed_config()`: `response` **Changed**
    - `experimental_mcp_enabled` **Added**
    - `experimental_mcp_events_schema` **Added**
    - `publish_aws_sqs_role_arn` **Added**
* `outpost.configuration.update_managed_config()`: 
  * `request` **Changed**
    - `experimental_mcp_enabled` **Added**
    - `experimental_mcp_events_schema` **Added**
    - `publish_aws_sqs_role_arn` **Added**
  * `response` **Changed**
    - `experimental_mcp_enabled` **Added**
    - `experimental_mcp_events_schema` **Added**
    - `publish_aws_sqs_role_arn` **Added**
* `outpost.operator_events.list_destinations()`:  `response.[].union(webhook).credentials.custom_headers` **Added**
* `outpost.operator_events.create_destination()`:  `response.union(webhook).credentials.custom_headers` **Added**
* `outpost.operator_events.get_destination()`:  `response.union(webhook).credentials.custom_headers` **Added**
* `outpost.operator_events.update_destination()`:  `response.union(webhook).credentials.custom_headers` **Added**
* `outpost.operator_events.enable_destination()`:  `response.union(webhook).credentials.custom_headers` **Added**
* `outpost.operator_events.disable_destination()`:  `response.union(webhook).credentials.custom_headers` **Added**
* `outpost.tenants.get_portal_url()`:  `error.status[501]` **Added**
* `outpost.tenants.get_token()`:  `error.status[501]` **Added**
* `outpost.destinations.list()`:  `response.[].union(webhook).credentials.custom_headers` **Added**
* `outpost.destinations.create()`: 
  *  `request.body.union(webhook).credentials.custom_headers` **Added**
  *  `response.union(webhook).credentials.custom_headers` **Added**
* `outpost.destinations.get()`:  `response.union(webhook).credentials.custom_headers` **Added**
* `outpost.destinations.update()`: 
  *  `request.body.union(webhook).credentials.custom_headers` **Added**
  *  `response.union(Destination).union(webhook).credentials.custom_headers` **Added**
* `outpost.destinations.enable()`:  `response.union(webhook).credentials.custom_headers` **Added**
* `outpost.destinations.disable()`:  `response.union(webhook).credentials.custom_headers` **Added**
