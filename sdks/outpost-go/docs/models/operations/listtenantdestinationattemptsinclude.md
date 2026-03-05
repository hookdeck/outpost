# ListTenantDestinationAttemptsInclude

Fields to include in the response. Can be specified multiple times or comma-separated.
- `event`: Include event summary (id, topic, time, eligible_for_retry, metadata)
- `event.data`: Include full event with payload data
- `response_data`: Include response body and headers



## Supported Types

### 

```go
listTenantDestinationAttemptsInclude := operations.CreateListTenantDestinationAttemptsIncludeStr(string{/* values here */})
```

### 

```go
listTenantDestinationAttemptsInclude := operations.CreateListTenantDestinationAttemptsIncludeArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch listTenantDestinationAttemptsInclude.Type {
	case operations.ListTenantDestinationAttemptsIncludeTypeStr:
		// listTenantDestinationAttemptsInclude.Str is populated
	case operations.ListTenantDestinationAttemptsIncludeTypeArrayOfStr:
		// listTenantDestinationAttemptsInclude.ArrayOfStr is populated
}
```
