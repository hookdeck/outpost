/*
 * Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.
 */

import { schemasListTenantDestinationTypes } from "../../funcs/schemasListTenantDestinationTypes.js";
import * as operations from "../../models/operations/index.js";
import { formatResult, ToolDefinition } from "../tools.js";

const args = {
  request: operations.ListTenantDestinationTypeSchemasRequest$inboundSchema,
};

export const tool$schemasListTenantDestinationTypes: ToolDefinition<
  typeof args
> = {
  name: "schemas-list-tenant-destination-types",
  description: `List Destination Type Schemas (for Tenant)

Returns a list of JSON-based input schemas for each available destination type. Requires Admin API Key or Tenant JWT.`,
  args,
  tool: async (client, args, ctx) => {
    const [result, apiCall] = await schemasListTenantDestinationTypes(
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
