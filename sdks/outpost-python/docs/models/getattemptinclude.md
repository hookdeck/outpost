# GetAttemptInclude

Fields to include in the response. Use bracket notation for multiple values (e.g., `include[0]=event&include[1]=response_data`).
- `event`: Include event summary (id, topic, time, eligible_for_retry, metadata)
- `event.data`: Include full event with payload data
- `response_data`: Include response body and headers



## Supported Types

### `str`

```python
value: str = /* values here */
```

### `List[str]`

```python
value: List[str] = /* values here */
```

