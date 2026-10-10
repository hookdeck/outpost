// Destination type and status helpers. This module has no runtime imports so
// `npm test` can load it in plain Node.
import type {
  Destination,
  DestinationTypeReference,
} from "../typings/Destination";

export const MCP_DESTINATION_TYPE = "mcp";

// isFormType reports whether the portal may offer a create form for the type.
// An absent or empty create_mode (API v1, older servers) means "form"; any
// other value than "form" means the type is created outside the portal.
export function isFormType(
  type: Pick<DestinationTypeReference, "create_mode">,
): boolean {
  const mode = type.create_mode;
  return mode === undefined || mode === null || mode === "" || mode === "form";
}

export function isExternalType(
  type: Pick<DestinationTypeReference, "create_mode">,
): boolean {
  return !isFormType(type);
}

// isMCPDestinationType reports whether destinations of a type are read-only in
// the portal: the mcp type, or any external type. The type reference may be
// missing (unknown or hidden type), so the name is checked on its own.
export function isMCPDestinationType(
  typeName: string,
  type: Pick<DestinationTypeReference, "create_mode"> | undefined,
): boolean {
  return (
    typeName === MCP_DESTINATION_TYPE ||
    (type !== undefined && isExternalType(type))
  );
}

// lookupType reads a type by name without falling through to Object.prototype
// (a type named "constructor" must not resolve to a function).
export function lookupType(
  types: Record<string, DestinationTypeReference> | undefined,
  name: string | null | undefined,
): DestinationTypeReference | undefined {
  if (!types || !name || !Object.hasOwn(types, name)) {
    return undefined;
  }
  return types[name];
}

// typeLabel names a destination's type, also when its reference is missing.
export function typeLabel(
  typeName: string,
  type: Pick<DestinationTypeReference, "label"> | undefined,
): string {
  if (type?.label) {
    return type.label;
  }
  return typeName === MCP_DESTINATION_TYPE ? "MCP" : typeName;
}

export function indexTypes(
  types: DestinationTypeReference[],
): Record<string, DestinationTypeReference> {
  const index: Record<string, DestinationTypeReference> = {};
  for (const type of types) {
    // defineProperty rather than assignment: a "__proto__" key must not set
    // the prototype.
    Object.defineProperty(index, type.type, {
      value: type,
      enumerable: true,
      writable: true,
      configurable: true,
    });
  }
  return index;
}

// parseShowMCPDestinations reads PORTAL_CONFIGS.SHOW_MCP_DESTINATIONS, which
// defaults to true when absent or empty.
export function parseShowMCPDestinations(value: unknown): boolean {
  if (typeof value === "boolean") {
    return value;
  }
  if (typeof value !== "string") {
    return true;
  }
  const normalized = value.trim().toLowerCase();
  return normalized !== "false" && normalized !== "0";
}

// visibleTypes drops MCP types when the portal hides MCP destinations.
export function visibleTypes(
  types: DestinationTypeReference[],
  showMCP: boolean,
): DestinationTypeReference[] {
  if (showMCP) {
    return types;
  }
  return types.filter((type) => !isMCPDestinationType(type.type, type));
}

// isDestinationVisible hides MCP destinations when the portal hides them. It
// takes every type, hidden ones included, to recognize external types.
export function isDestinationVisible(
  destination: Pick<Destination, "type">,
  allTypes: Record<string, DestinationTypeReference> | undefined,
  showMCP: boolean,
): boolean {
  return (
    showMCP ||
    !isMCPDestinationType(
      destination.type,
      lookupType(allTypes, destination.type),
    )
  );
}

// isExpired reports whether expiresAt is at or before now. A missing or
// unparsable value never expires.
export function isExpired(
  expiresAt: string | null | undefined,
  now: number,
): boolean {
  if (!expiresAt) {
    return false;
  }
  const time = Date.parse(expiresAt);
  return !Number.isNaN(time) && time <= now;
}

export type DestinationStatus = "active" | "disabled" | "expired";

// destinationStatus puts expiry first: an expired destination gets no
// deliveries whether or not it is disabled.
export function destinationStatus(
  destination: Pick<Destination, "disabled_at" | "expires_at">,
  now: number,
): DestinationStatus {
  if (isExpired(destination.expires_at, now)) {
    return "expired";
  }
  return destination.disabled_at ? "disabled" : "active";
}

// configString reads a string config value, or undefined when the key is
// missing or isn't a string.
export function configString(
  destination: Pick<Destination, "config">,
  key: string,
): string | undefined {
  const config: unknown = destination.config;
  if (typeof config !== "object" || config === null) {
    return undefined;
  }
  if (!Object.hasOwn(config, key)) {
    return undefined;
  }
  const value = (config as Record<string, unknown>)[key];
  return typeof value === "string" ? value : undefined;
}

// mcpDeliveries names what an MCP destination delivers, "<event> events to
// <callback URL>". Disconnecting it stops only these: an agent usually holds
// one subscription per event, and the others keep receiving events.
export function mcpDeliveries(
  destination: Pick<Destination, "config" | "topics">,
): string {
  const event = configString(destination, "event") ?? destination.topics?.[0];
  const url = configString(destination, "url");
  const events = event ? `${event} events` : "events";
  return url ? `${events} to ${url}` : events;
}

export type ParsedArguments =
  | { ok: true; value: unknown }
  | { ok: false; raw: string };

// parseArguments decodes an MCP destination's config.arguments, a JSON string.
// It returns null when there are none, and the raw string when it isn't JSON.
export function parseArguments(raw: unknown): ParsedArguments | null {
  if (raw === undefined || raw === null || raw === "") {
    return null;
  }
  if (typeof raw !== "string") {
    return { ok: true, value: raw };
  }
  try {
    return { ok: true, value: JSON.parse(raw) };
  } catch {
    return { ok: false, raw };
  }
}
