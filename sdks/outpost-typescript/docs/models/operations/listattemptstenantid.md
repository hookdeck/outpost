# ListAttemptsTenantId

Filter attempts by tenant ID(s). Use bracket notation for multiple values (e.g., `tenant_id[0]=t1&tenant_id[1]=t2`).
When authenticated with a Tenant JWT, this parameter is ignored and the JWT's tenant is used.
If not provided with API key auth, returns attempts from all tenants.



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

