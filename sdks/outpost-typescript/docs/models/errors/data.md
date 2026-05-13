# Data

Additional error details. For validation errors, this is an array of human-readable messages.


## Supported Types

### `string[]`

```typescript
const value: string[] = [
  "email is required",
  "password must be at least 6 characters",
];
```

### `{ [k: string]: any }`

```typescript
const value: { [k: string]: any } = {
  "key": "<value>",
  "key1": "<value>",
  "key2": "<value>",
};
```

