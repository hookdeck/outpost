# ListTenantDestinationAttemptsTopic

Filter attempts by event topic(s). Can be specified multiple times or comma-separated.


## Supported Types

### 

```go
listTenantDestinationAttemptsTopic := operations.CreateListTenantDestinationAttemptsTopicStr(string{/* values here */})
```

### 

```go
listTenantDestinationAttemptsTopic := operations.CreateListTenantDestinationAttemptsTopicArrayOfStr([]string{/* values here */})
```

## Union Discrimination

Use the `Type` field to determine which variant is active, then access the corresponding field:

```go
switch listTenantDestinationAttemptsTopic.Type {
	case operations.ListTenantDestinationAttemptsTopicTypeStr:
		// listTenantDestinationAttemptsTopic.Str is populated
	case operations.ListTenantDestinationAttemptsTopicTypeArrayOfStr:
		// listTenantDestinationAttemptsTopic.ArrayOfStr is populated
}
```
