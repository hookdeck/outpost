# Hookdeck Configuration Instructions

The [Hookdeck Event Gateway](https://hookdeck.com?ref=outpost-portal) is a reliable event management platform that ensures your webhooks and events are properly delivered to your endpoints. It provides features such as:

- Event buffering to handle traffic spikes
- Automatic retries for failed deliveries
- Event logs and monitoring
- Request filtering and transformation
- Enhanced security for your webhook endpoints
- Alert notifications and issue management

## How to configure Hookdeck as an event destination

1. Click **Generate Hookdeck Token** to open the Hookdeck Dashboard **Create Managed Source** flow
2. In the **Create Managed Source** page, enter a name for your Outpost source. Click **Allow**.
3. You will then be presented with a **Source Token**. Copy this value.
4. Within the Outpost portal, paste the source token into the **Hookdeck Token** field. Click **Create Destination** to create the destination.
5. Go back to the Hookdeck Dashboard and click **Create Connection**.
6. In the **Create Connection** page, you can configure how you want Hookdeck to handle your event including transforming the payload, filtering and routing events, and reliably delivering them to one or more HTTP endpoints.

See the [Hookdeck Connections docs](https://hookdeck.com/docs/connections?ref=outpost-portal) for more information.

