/*
 * Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.
 */

import { eventsGetByDestination } from "../../funcs/eventsGetByDestination.js";
import * as operations from "../../models/operations/index.js";
import { formatResult, ToolDefinition } from "../tools.js";

const args = {
  request: operations.GetTenantEventByDestinationRequest$inboundSchema,
};

export const tool$eventsGetByDestination: ToolDefinition<typeof args> = {
  name: "events-get-by-destination",
  description: `Get Event by Destination

Retrieves a specific event associated with a specific destination for the tenant.`,
  args,
  tool: async (client, args, ctx) => {
    const [result, apiCall] = await eventsGetByDestination(
      client,
      args.request,
      { fetchOptions: { signal: ctx.signal } },
    ).$inspect();

    if (!result.ok) {
      return {
        content: [{ type: "text", text: result.error.message }],
        isError: true,
      };
    }

    const value = result.value;

    return formatResult(value, apiCall);
  },
};
