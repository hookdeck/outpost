/*
 * Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.
 */

import * as z from "zod";
import { remap as remap$ } from "../../lib/primitives.js";
import { safeParse } from "../../lib/schemas.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";

export type ListTenantDestinationTypeSchemasGlobals = {
  tenantId?: string | undefined;
};

export type ListTenantDestinationTypeSchemasRequest = {
  /**
   * The ID of the tenant. Required when using AdminApiKey authentication.
   */
  tenantId?: string | undefined;
};

/** @internal */
export const ListTenantDestinationTypeSchemasGlobals$inboundSchema: z.ZodType<
  ListTenantDestinationTypeSchemasGlobals,
  z.ZodTypeDef,
  unknown
> = z.object({
  tenant_id: z.string().optional(),
}).transform((v) => {
  return remap$(v, {
    "tenant_id": "tenantId",
  });
});

/** @internal */
export type ListTenantDestinationTypeSchemasGlobals$Outbound = {
  tenant_id?: string | undefined;
};

/** @internal */
export const ListTenantDestinationTypeSchemasGlobals$outboundSchema: z.ZodType<
  ListTenantDestinationTypeSchemasGlobals$Outbound,
  z.ZodTypeDef,
  ListTenantDestinationTypeSchemasGlobals
> = z.object({
  tenantId: z.string().optional(),
}).transform((v) => {
  return remap$(v, {
    tenantId: "tenant_id",
  });
});

/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export namespace ListTenantDestinationTypeSchemasGlobals$ {
  /** @deprecated use `ListTenantDestinationTypeSchemasGlobals$inboundSchema` instead. */
  export const inboundSchema =
    ListTenantDestinationTypeSchemasGlobals$inboundSchema;
  /** @deprecated use `ListTenantDestinationTypeSchemasGlobals$outboundSchema` instead. */
  export const outboundSchema =
    ListTenantDestinationTypeSchemasGlobals$outboundSchema;
  /** @deprecated use `ListTenantDestinationTypeSchemasGlobals$Outbound` instead. */
  export type Outbound = ListTenantDestinationTypeSchemasGlobals$Outbound;
}

export function listTenantDestinationTypeSchemasGlobalsToJSON(
  listTenantDestinationTypeSchemasGlobals:
    ListTenantDestinationTypeSchemasGlobals,
): string {
  return JSON.stringify(
    ListTenantDestinationTypeSchemasGlobals$outboundSchema.parse(
      listTenantDestinationTypeSchemasGlobals,
    ),
  );
}

export function listTenantDestinationTypeSchemasGlobalsFromJSON(
  jsonString: string,
): SafeParseResult<
  ListTenantDestinationTypeSchemasGlobals,
  SDKValidationError
> {
  return safeParse(
    jsonString,
    (x) =>
      ListTenantDestinationTypeSchemasGlobals$inboundSchema.parse(
        JSON.parse(x),
      ),
    `Failed to parse 'ListTenantDestinationTypeSchemasGlobals' from JSON`,
  );
}

/** @internal */
export const ListTenantDestinationTypeSchemasRequest$inboundSchema: z.ZodType<
  ListTenantDestinationTypeSchemasRequest,
  z.ZodTypeDef,
  unknown
> = z.object({
  tenant_id: z.string().optional(),
}).transform((v) => {
  return remap$(v, {
    "tenant_id": "tenantId",
  });
});

/** @internal */
export type ListTenantDestinationTypeSchemasRequest$Outbound = {
  tenant_id?: string | undefined;
};

/** @internal */
export const ListTenantDestinationTypeSchemasRequest$outboundSchema: z.ZodType<
  ListTenantDestinationTypeSchemasRequest$Outbound,
  z.ZodTypeDef,
  ListTenantDestinationTypeSchemasRequest
> = z.object({
  tenantId: z.string().optional(),
}).transform((v) => {
  return remap$(v, {
    tenantId: "tenant_id",
  });
});

/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export namespace ListTenantDestinationTypeSchemasRequest$ {
  /** @deprecated use `ListTenantDestinationTypeSchemasRequest$inboundSchema` instead. */
  export const inboundSchema =
    ListTenantDestinationTypeSchemasRequest$inboundSchema;
  /** @deprecated use `ListTenantDestinationTypeSchemasRequest$outboundSchema` instead. */
  export const outboundSchema =
    ListTenantDestinationTypeSchemasRequest$outboundSchema;
  /** @deprecated use `ListTenantDestinationTypeSchemasRequest$Outbound` instead. */
  export type Outbound = ListTenantDestinationTypeSchemasRequest$Outbound;
}

export function listTenantDestinationTypeSchemasRequestToJSON(
  listTenantDestinationTypeSchemasRequest:
    ListTenantDestinationTypeSchemasRequest,
): string {
  return JSON.stringify(
    ListTenantDestinationTypeSchemasRequest$outboundSchema.parse(
      listTenantDestinationTypeSchemasRequest,
    ),
  );
}

export function listTenantDestinationTypeSchemasRequestFromJSON(
  jsonString: string,
): SafeParseResult<
  ListTenantDestinationTypeSchemasRequest,
  SDKValidationError
> {
  return safeParse(
    jsonString,
    (x) =>
      ListTenantDestinationTypeSchemasRequest$inboundSchema.parse(
        JSON.parse(x),
      ),
    `Failed to parse 'ListTenantDestinationTypeSchemasRequest' from JSON`,
  );
}
