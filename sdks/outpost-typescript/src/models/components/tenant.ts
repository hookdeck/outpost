/*
 * Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.
 */

import * as z from "zod";
import { remap as remap$ } from "../../lib/primitives.js";
import { safeParse } from "../../lib/schemas.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";

export type Tenant = {
  /**
   * User-defined system ID for the tenant.
   */
  id?: string | undefined;
  /**
   * Number of destinations associated with the tenant.
   */
  destinationsCount?: number | undefined;
  /**
   * List of subscribed topics across all destinations for this tenant.
   */
  topics?: Array<string> | undefined;
  /**
   * ISO Date when the tenant was created.
   */
  createdAt?: Date | undefined;
};

/** @internal */
export const Tenant$inboundSchema: z.ZodType<Tenant, z.ZodTypeDef, unknown> = z
  .object({
    id: z.string().optional(),
    destinations_count: z.number().int().optional(),
    topics: z.array(z.string()).optional(),
    created_at: z.string().datetime({ offset: true }).transform(v =>
      new Date(v)
    ).optional(),
  }).transform((v) => {
    return remap$(v, {
      "destinations_count": "destinationsCount",
      "created_at": "createdAt",
    });
  });

/** @internal */
export type Tenant$Outbound = {
  id?: string | undefined;
  destinations_count?: number | undefined;
  topics?: Array<string> | undefined;
  created_at?: string | undefined;
};

/** @internal */
export const Tenant$outboundSchema: z.ZodType<
  Tenant$Outbound,
  z.ZodTypeDef,
  Tenant
> = z.object({
  id: z.string().optional(),
  destinationsCount: z.number().int().optional(),
  topics: z.array(z.string()).optional(),
  createdAt: z.date().transform(v => v.toISOString()).optional(),
}).transform((v) => {
  return remap$(v, {
    destinationsCount: "destinations_count",
    createdAt: "created_at",
  });
});

/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export namespace Tenant$ {
  /** @deprecated use `Tenant$inboundSchema` instead. */
  export const inboundSchema = Tenant$inboundSchema;
  /** @deprecated use `Tenant$outboundSchema` instead. */
  export const outboundSchema = Tenant$outboundSchema;
  /** @deprecated use `Tenant$Outbound` instead. */
  export type Outbound = Tenant$Outbound;
}

export function tenantToJSON(tenant: Tenant): string {
  return JSON.stringify(Tenant$outboundSchema.parse(tenant));
}

export function tenantFromJSON(
  jsonString: string,
): SafeParseResult<Tenant, SDKValidationError> {
  return safeParse(
    jsonString,
    (x) => Tenant$inboundSchema.parse(JSON.parse(x)),
    `Failed to parse 'Tenant' from JSON`,
  );
}
