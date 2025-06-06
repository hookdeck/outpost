/*
 * Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT.
 */

import * as z from "zod";
import { remap as remap$ } from "../../lib/primitives.js";
import { safeParse } from "../../lib/schemas.js";
import { ClosedEnum } from "../../types/enums.js";
import { Result as SafeParseResult } from "../../types/fp.js";
import { SDKValidationError } from "../errors/sdkvalidationerror.js";

/**
 * Whether to use TLS connection (amqps). Defaults to "false".
 */
export const Tls = {
  True: "true",
  False: "false",
} as const;
/**
 * Whether to use TLS connection (amqps). Defaults to "false".
 */
export type Tls = ClosedEnum<typeof Tls>;

export type RabbitMQConfig = {
  /**
   * RabbitMQ server address (host:port).
   */
  serverUrl: string;
  /**
   * The exchange to publish messages to.
   */
  exchange: string;
  /**
   * Whether to use TLS connection (amqps). Defaults to "false".
   */
  tls?: Tls | undefined;
};

/** @internal */
export const Tls$inboundSchema: z.ZodNativeEnum<typeof Tls> = z.nativeEnum(Tls);

/** @internal */
export const Tls$outboundSchema: z.ZodNativeEnum<typeof Tls> =
  Tls$inboundSchema;

/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export namespace Tls$ {
  /** @deprecated use `Tls$inboundSchema` instead. */
  export const inboundSchema = Tls$inboundSchema;
  /** @deprecated use `Tls$outboundSchema` instead. */
  export const outboundSchema = Tls$outboundSchema;
}

/** @internal */
export const RabbitMQConfig$inboundSchema: z.ZodType<
  RabbitMQConfig,
  z.ZodTypeDef,
  unknown
> = z.object({
  server_url: z.string(),
  exchange: z.string(),
  tls: Tls$inboundSchema.optional(),
}).transform((v) => {
  return remap$(v, {
    "server_url": "serverUrl",
  });
});

/** @internal */
export type RabbitMQConfig$Outbound = {
  server_url: string;
  exchange: string;
  tls?: string | undefined;
};

/** @internal */
export const RabbitMQConfig$outboundSchema: z.ZodType<
  RabbitMQConfig$Outbound,
  z.ZodTypeDef,
  RabbitMQConfig
> = z.object({
  serverUrl: z.string(),
  exchange: z.string(),
  tls: Tls$outboundSchema.optional(),
}).transform((v) => {
  return remap$(v, {
    serverUrl: "server_url",
  });
});

/**
 * @internal
 * @deprecated This namespace will be removed in future versions. Use schemas and types that are exported directly from this module.
 */
export namespace RabbitMQConfig$ {
  /** @deprecated use `RabbitMQConfig$inboundSchema` instead. */
  export const inboundSchema = RabbitMQConfig$inboundSchema;
  /** @deprecated use `RabbitMQConfig$outboundSchema` instead. */
  export const outboundSchema = RabbitMQConfig$outboundSchema;
  /** @deprecated use `RabbitMQConfig$Outbound` instead. */
  export type Outbound = RabbitMQConfig$Outbound;
}

export function rabbitMQConfigToJSON(rabbitMQConfig: RabbitMQConfig): string {
  return JSON.stringify(RabbitMQConfig$outboundSchema.parse(rabbitMQConfig));
}

export function rabbitMQConfigFromJSON(
  jsonString: string,
): SafeParseResult<RabbitMQConfig, SDKValidationError> {
  return safeParse(
    jsonString,
    (x) => RabbitMQConfig$inboundSchema.parse(JSON.parse(x)),
    `Failed to parse 'RabbitMQConfig' from JSON`,
  );
}
