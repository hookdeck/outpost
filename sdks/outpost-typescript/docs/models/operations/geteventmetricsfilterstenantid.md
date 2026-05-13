# GetEventMetricsFiltersTenantId

Filter by tenant ID(s). Admin-only — rejected with 403 for JWT callers. Use bracket notation for multiple values (e.g., `filters[tenant_id][0]=t1&filters[tenant_id][1]=t2`).


## Supported Types

### `string`

```typescript
const value: string = "<value>";
```

### `string[]`

```typescript
const value: string[] = [
  "<value 1>",
  "<value 2>",
];
```

