## Go SDK Changes:
* `Outpost.Configuration.GetManagedConfig()`: `response` **Changed**
    - `ExperimentalMcpEnabled` **Added**
    - `ExperimentalMcpEventsSchema` **Added**
    - `PublishAwsSqsRoleArn` **Added**
* `Outpost.Configuration.UpdateManagedConfig()`: 
  * `request.Request` **Changed**
    - `ExperimentalMcpEnabled` **Added**
    - `ExperimentalMcpEventsSchema` **Added**
    - `PublishAwsSqsRoleArn` **Added**
  * `response` **Changed**
    - `ExperimentalMcpEnabled` **Added**
    - `ExperimentalMcpEventsSchema` **Added**
    - `PublishAwsSqsRoleArn` **Added**
* `Outpost.OperatorEvents.ListDestinations()`:  `response.[].union(webhook).Credentials.CustomHeaders` **Added**
* `Outpost.OperatorEvents.CreateDestination()`:  `response.union(webhook).Credentials.CustomHeaders` **Added**
* `Outpost.OperatorEvents.GetDestination()`:  `response.union(webhook).Credentials.CustomHeaders` **Added**
* `Outpost.OperatorEvents.UpdateDestination()`:  `response.union(webhook).Credentials.CustomHeaders` **Added**
* `Outpost.OperatorEvents.EnableDestination()`:  `response.union(webhook).Credentials.CustomHeaders` **Added**
* `Outpost.OperatorEvents.DisableDestination()`:  `response.union(webhook).Credentials.CustomHeaders` **Added**
* `Outpost.Tenants.GetPortalUrl()`:  `error.status[501]` **Added**
* `Outpost.Tenants.GetToken()`:  `error.status[501]` **Added**
* `Outpost.Destinations.List()`:  `response.[].union(webhook).Credentials.CustomHeaders` **Added**
* `Outpost.Destinations.Create()`: 
  *  `request.Body.union(webhook).Credentials.CustomHeaders` **Added**
  *  `response.union(webhook).Credentials.CustomHeaders` **Added**
* `Outpost.Destinations.Get()`:  `response.union(webhook).Credentials.CustomHeaders` **Added**
* `Outpost.Destinations.Update()`: 
  *  `request.Body.union(webhook).Credentials.CustomHeaders` **Added**
  *  `response.union(Destination).union(webhook).Credentials.CustomHeaders` **Added**
* `Outpost.Destinations.Enable()`:  `response.union(webhook).Credentials.CustomHeaders` **Added**
* `Outpost.Destinations.Disable()`:  `response.union(webhook).Credentials.CustomHeaders` **Added**
