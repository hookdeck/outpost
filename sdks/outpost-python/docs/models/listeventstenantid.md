# ListEventsTenantID

Filter events by tenant ID(s). Use bracket notation for multiple values (e.g., `tenant_id[0]=t1&tenant_id[1]=t2`).
When authenticated with a Tenant JWT, this parameter is ignored and the JWT's tenant is used.
If not provided with API key auth, returns events from all tenants.



## Supported Types

### `str`

```python
value: str = /* values here */
```

### `List[str]`

```python
value: List[str] = /* values here */
```

