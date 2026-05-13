# SetupLink

Some destinations may have an OAuth flow or other managed-setup flow that can be triggered with a link. If a `setup_link` is set then the user should be prompted to follow the link to configure the destination.
See the [building your own UI guide](https://outpost.hookdeck.com/guides/building-your-own-ui.mdx) for recommended UI patterns and wireframes for implementation in your own app.

## Example Usage

```typescript
import { SetupLink } from "@hookdeck/outpost-sdk/models/components";

let value: SetupLink = {
  href: "https://dashboard.hookdeck.com/connect",
  cta: "Generate Hookdeck Token",
};
```

## Fields

| Field                                                  | Type                                                   | Required                                               | Description                                            | Example                                                |
| ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ |
| `href`                                                 | *string*                                               | :heavy_check_mark:                                     | The URL to direct the user to for setup.               | https://dashboard.hookdeck.com/connect                 |
| `cta`                                                  | *string*                                               | :heavy_check_mark:                                     | The call-to-action button text to display to the user. | Generate Hookdeck Token                                |