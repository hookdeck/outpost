/*
 * Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.
 */

import * as z from "zod";
import { remap as remap$ } from "../../lib/primitives.js";
import { safeParse } from "../../lib/schemas.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";
import {
  DestinationSchemaField,
  DestinationSchemaField$inboundSchema,
  DestinationSchemaField$Outbound,
  DestinationSchemaField$outboundSchema,
} from "./destinationschemafield.js";

export type DestinationTypeSchema = {
  type?: string | undefined;
  label?: string | undefined;
  description?: string | undefined;
  /**
   * SVG icon string.
   */
  icon?: string | undefined;
  /**
   * Markdown instructions.
   */
  instructions?: string | undefined;
  /**
   * Some destinations may have Oauth flow or other managed-setup flow that can be triggered with a link. If a `remote_setup_url` is set then the user should be prompted to follow the link to configure the destination.
   *
   * @remarks
   * See the [building your own UI guide](https://outpost.hookdeck.com/guides/building-your-own-ui.mdx) for recommended UI patterns and wireframes for implementation in your own app.
   */
  remoteSetupUrl?: string | undefined;
  /**
   * Config fields are non-secret values that can be stored and displayed to the user in plain text.
   */
  configFields?: Array<DestinationSchemaField> | undefined;
  /**
   * Credential fields are secret values that will be AES encrypted and obfuscated to the user. Some credentials may not be obfuscated; the destination type dictates the obfuscation logic.
   */
  credentialFields?: Array<DestinationSchemaField> | undefined;
};

/** @internal */
export const DestinationTypeSchema$inboundSchema: z.ZodType<
  DestinationTypeSchema,
  z.ZodTypeDef,
  unknown
> = z.object({
  type: z.string().optional(),
  label: z.string().optional(),
  description: z.string().optional(),
  icon: z.string().optional(),
  instructions: z.string().optional(),
  remote_setup_url: z.string().optional(),
  config_fields: z.array(DestinationSchemaField$inboundSchema).optional(),
  credential_fields: z.array(DestinationSchemaField$inboundSchema).optional(),
}).transform((v) => {
  return remap$(v, {
    "remote_setup_url": "remoteSetupUrl",
    "config_fields": "configFields",
    "credential_fields": "credentialFields",
  });
});

/** @internal */
export type DestinationTypeSchema$Outbound = {
  type?: string | undefined;
  label?: string | undefined;
  description?: string | undefined;
  icon?: string | undefined;
  instructions?: string | undefined;
  remote_setup_url?: string | undefined;
  config_fields?: Array<DestinationSchemaField$Outbound> | undefined;
  credential_fields?: Array<DestinationSchemaField$Outbound> | undefined;
};

/** @internal */
export const DestinationTypeSchema$outboundSchema: z.ZodType<
  DestinationTypeSchema$Outbound,
  z.ZodTypeDef,
  DestinationTypeSchema
> = z.object({
  type: z.string().optional(),
  label: z.string().optional(),
  description: z.string().optional(),
  icon: z.string().optional(),
  instructions: z.string().optional(),
  remoteSetupUrl: z.string().optional(),
  configFields: z.array(DestinationSchemaField$outboundSchema).optional(),
  credentialFields: z.array(DestinationSchemaField$outboundSchema).optional(),
}).transform((v) => {
  return remap$(v, {
    remoteSetupUrl: "remote_setup_url",
    configFields: "config_fields",
    credentialFields: "credential_fields",
  });
});

/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export namespace DestinationTypeSchema$ {
  /** @deprecated use `DestinationTypeSchema$inboundSchema` instead. */
  export const inboundSchema = DestinationTypeSchema$inboundSchema;
  /** @deprecated use `DestinationTypeSchema$outboundSchema` instead. */
  export const outboundSchema = DestinationTypeSchema$outboundSchema;
  /** @deprecated use `DestinationTypeSchema$Outbound` instead. */
  export type Outbound = DestinationTypeSchema$Outbound;
}

export function destinationTypeSchemaToJSON(
  destinationTypeSchema: DestinationTypeSchema,
): string {
  return JSON.stringify(
    DestinationTypeSchema$outboundSchema.parse(destinationTypeSchema),
  );
}

export function destinationTypeSchemaFromJSON(
  jsonString: string,
): SafeParseResult<DestinationTypeSchema, SDKValidationError> {
  return safeParse(
    jsonString,
    (x) => DestinationTypeSchema$inboundSchema.parse(JSON.parse(x)),
    `Failed to parse 'DestinationTypeSchema' from JSON`,
  );
}
