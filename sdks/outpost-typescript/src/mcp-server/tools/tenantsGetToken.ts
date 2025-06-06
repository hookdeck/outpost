/*
 * Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.
 */

import { tenantsGetToken } from "../../funcs/tenantsGetToken.js";
import * as operations from "../../models/operations/index.js";
import { formatResult, ToolDefinition } from "../tools.js";

const args = {
  request: operations.GetTenantTokenRequest$inboundSchema,
};

export const tool$tenantsGetToken: ToolDefinition<typeof args> = {
  name: "tenants-get-token",
  description: `Get Tenant JWT Token

Returns a JWT token scoped to the tenant for safe browser API calls.`,
  args,
  tool: async (client, args, ctx) => {
    const [result, apiCall] = await tenantsGetToken(
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
