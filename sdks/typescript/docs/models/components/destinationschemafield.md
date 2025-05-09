# DestinationSchemaField

## Example Usage

```typescript
import { DestinationSchemaField } from "openapi/models/components";

let value: DestinationSchemaField = {
  type: "text",
  label: "URL",
  description: "The URL to send the event to",
  required: true,
  sensitive: false,
  default: "default_value",
  minlength: 0,
  maxlength: 255,
  pattern: "^[a-zA-Z0-9_]+$",
};
```

## Fields

| Field                                                                                          | Type                                                                                           | Required                                                                                       | Description                                                                                    | Example                                                                                        |
| ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------- |
| `type`                                                                                         | [components.DestinationSchemaFieldType](../../models/components/destinationschemafieldtype.md) | :heavy_check_mark:                                                                             | N/A                                                                                            | text                                                                                           |
| `label`                                                                                        | *string*                                                                                       | :heavy_minus_sign:                                                                             | N/A                                                                                            | URL                                                                                            |
| `description`                                                                                  | *string*                                                                                       | :heavy_minus_sign:                                                                             | N/A                                                                                            | The URL to send the event to                                                                   |
| `required`                                                                                     | *boolean*                                                                                      | :heavy_check_mark:                                                                             | N/A                                                                                            | true                                                                                           |
| `sensitive`                                                                                    | *boolean*                                                                                      | :heavy_minus_sign:                                                                             | Indicates if the field contains sensitive information.                                         | false                                                                                          |
| `default`                                                                                      | *string*                                                                                       | :heavy_minus_sign:                                                                             | Default value for the field.                                                                   | default_value                                                                                  |
| `minlength`                                                                                    | *number*                                                                                       | :heavy_minus_sign:                                                                             | Minimum length for a text input.                                                               | 0                                                                                              |
| `maxlength`                                                                                    | *number*                                                                                       | :heavy_minus_sign:                                                                             | Maximum length for a text input.                                                               | 255                                                                                            |
| `pattern`                                                                                      | *string*                                                                                       | :heavy_minus_sign:                                                                             | Regex pattern for validation (compatible with HTML5 pattern attribute).                        | ^[a-zA-Z0-9_]+$                                                                                |