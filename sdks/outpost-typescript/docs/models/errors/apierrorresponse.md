# APIErrorResponse

Standard error response format.

## Example Usage

```typescript
import { APIErrorResponse } from "@hookdeck/outpost-sdk/models/errors";

// No examples available for this model
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `status`                                                                                      | *number*                                                                                      | :heavy_minus_sign:                                                                            | HTTP status code.                                                                             | 422                                                                                           |
| `message`                                                                                     | *string*                                                                                      | :heavy_minus_sign:                                                                            | Human-readable error message.                                                                 | validation error                                                                              |
| `data`                                                                                        | *errors.Data*                                                                                 | :heavy_minus_sign:                                                                            | Additional error details. For validation errors, this is an array of human-readable messages. |                                                                                               |