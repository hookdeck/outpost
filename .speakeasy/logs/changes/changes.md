## Typescript SDK Changes:
* `outpost.configuration.getManagedConfig()`: `response` **Changed**
    - `experimentalMcpEnabled` **Added**
    - `experimentalMcpEventsSchema` **Added**
    - `publishAwsSqsRoleArn` **Added**
* `outpost.configuration.updateManagedConfig()`: 
  * `request` **Changed**
    - `experimentalMcpEnabled` **Added**
    - `experimentalMcpEventsSchema` **Added**
    - `publishAwsSqsRoleArn` **Added**
  * `response` **Changed**
    - `experimentalMcpEnabled` **Added**
    - `experimentalMcpEventsSchema` **Added**
    - `publishAwsSqsRoleArn` **Added**
* `outpost.operatorEvents.listDestinations()`:  `response.[].union(webhook).credentials.customHeaders` **Added**
* `outpost.operatorEvents.createDestination()`:  `response.union(webhook).credentials.customHeaders` **Added**
* `outpost.operatorEvents.getDestination()`:  `response.union(webhook).credentials.customHeaders` **Added**
* `outpost.operatorEvents.updateDestination()`:  `response.union(webhook).credentials.customHeaders` **Added**
* `outpost.operatorEvents.enableDestination()`:  `response.union(webhook).credentials.customHeaders` **Added**
* `outpost.operatorEvents.disableDestination()`:  `response.union(webhook).credentials.customHeaders` **Added**
* `outpost.tenants.getPortalUrl()`:  `error.status[501]` **Added**
* `outpost.tenants.getToken()`:  `error.status[501]` **Added**
* `outpost.destinations.list()`:  `response.[].union(webhook).credentials.customHeaders` **Added**
* `outpost.destinations.create()`: 
  *  `request.body.union(webhook).credentials.customHeaders` **Added**
  *  `response.union(webhook).credentials.customHeaders` **Added**
* `outpost.destinations.get()`:  `response.union(webhook).credentials.customHeaders` **Added**
* `outpost.destinations.update()`: 
  *  `request.body.union(webhook).credentials.customHeaders` **Added**
  *  `response.union(Destination).union(webhook).credentials.customHeaders` **Added**
* `outpost.destinations.enable()`:  `response.union(webhook).credentials.customHeaders` **Added**
* `outpost.destinations.disable()`:  `response.union(webhook).credentials.customHeaders` **Added**
