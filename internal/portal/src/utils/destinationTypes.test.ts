import assert from "node:assert/strict";
import { describe, it } from "node:test";
import type { DestinationTypeReference } from "../typings/Destination";
import {
  configString,
  destinationStatus,
  indexTypes,
  isDestinationVisible,
  isExpired,
  isExternalType,
  isFormType,
  isMCPDestinationType,
  lookupType,
  parseArguments,
  parseShowMCPDestinations,
  typeLabel,
  visibleTypes,
} from "./destinationTypes.ts";

function typeRef(type: string, create_mode?: string): DestinationTypeReference {
  return {
    type,
    create_mode,
    config_fields: [],
    credential_fields: [],
    instructions: "",
    label: type,
    description: "",
    icon: "",
  };
}

const webhook = typeRef("webhook");
const webhookForm = typeRef("webhook", "form");
const mcp = typeRef("mcp", "external");
// A future external type that isn't mcp.
const otherExternal = typeRef("agent", "external");

describe("isFormType", () => {
  it("treats an absent or empty create_mode as form", () => {
    assert.equal(isFormType({}), true);
    assert.equal(isFormType({ create_mode: "" }), true);
    assert.equal(isFormType({ create_mode: null as unknown as string }), true);
    assert.equal(isFormType({ create_mode: "form" }), true);
  });

  it("offers no form for any other mode", () => {
    assert.equal(isFormType({ create_mode: "external" }), false);
    assert.equal(isFormType({ create_mode: "oauth" }), false);
    assert.equal(isFormType({ create_mode: "FORM" }), false);
    assert.equal(isExternalType({ create_mode: "external" }), true);
    assert.equal(isExternalType({}), false);
  });
});

describe("isMCPDestinationType", () => {
  it("recognizes mcp by name, even without its type reference", () => {
    assert.equal(isMCPDestinationType("mcp", undefined), true);
    assert.equal(isMCPDestinationType("mcp", mcp), true);
  });

  it("recognizes any external type", () => {
    assert.equal(isMCPDestinationType("agent", otherExternal), true);
  });

  it("leaves form and unknown types alone", () => {
    assert.equal(isMCPDestinationType("webhook", webhook), false);
    assert.equal(isMCPDestinationType("webhook", webhookForm), false);
    assert.equal(isMCPDestinationType("unknown", undefined), false);
  });
});

describe("lookupType and indexTypes", () => {
  const types = indexTypes([webhook, mcp]);

  it("finds indexed types", () => {
    assert.equal(lookupType(types, "webhook"), webhook);
    assert.equal(lookupType(types, "mcp"), mcp);
  });

  it("never resolves Object.prototype members", () => {
    for (const name of [
      "constructor",
      "toString",
      "__proto__",
      "hasOwnProperty",
    ]) {
      assert.equal(lookupType(types, name), undefined, name);
    }
  });

  it("handles a missing map or name", () => {
    assert.equal(lookupType(undefined, "webhook"), undefined);
    assert.equal(lookupType(types, undefined), undefined);
    assert.equal(lookupType(types, null), undefined);
    assert.equal(lookupType(types, ""), undefined);
  });

  it("indexes a __proto__ type as a plain key", () => {
    const proto = typeRef("__proto__");
    const index = indexTypes([proto]);
    assert.equal(Object.getPrototypeOf(index), Object.prototype);
    assert.equal(lookupType(index, "__proto__"), proto);
    assert.deepEqual(Object.keys(index), ["__proto__"]);
  });
});

describe("typeLabel", () => {
  it("uses the type's label, else a fallback", () => {
    assert.equal(typeLabel("webhook", { label: "Webhook" }), "Webhook");
    assert.equal(typeLabel("mcp", undefined), "MCP");
    assert.equal(typeLabel("custom", undefined), "custom");
    assert.equal(typeLabel("custom", { label: "" }), "custom");
  });
});

describe("parseShowMCPDestinations", () => {
  it("defaults to true when absent", () => {
    assert.equal(parseShowMCPDestinations(undefined), true);
    assert.equal(parseShowMCPDestinations(null), true);
    assert.equal(parseShowMCPDestinations(""), true);
  });

  it("reads the Go booleans and common spellings", () => {
    assert.equal(parseShowMCPDestinations("true"), true);
    assert.equal(parseShowMCPDestinations("false"), false);
    assert.equal(parseShowMCPDestinations(" FALSE "), false);
    assert.equal(parseShowMCPDestinations("0"), false);
    assert.equal(parseShowMCPDestinations(true), true);
    assert.equal(parseShowMCPDestinations(false), false);
  });
});

describe("visibleTypes", () => {
  const all = [webhook, mcp, otherExternal, webhookForm];

  it("keeps every type when MCP destinations are shown", () => {
    assert.deepEqual(visibleTypes(all, true), all);
  });

  it("drops mcp and external types when they are hidden", () => {
    assert.deepEqual(visibleTypes(all, false), [webhook, webhookForm]);
  });
});

describe("isDestinationVisible", () => {
  const types = indexTypes([webhook, mcp, otherExternal]);

  it("shows everything when MCP destinations are shown", () => {
    for (const type of ["webhook", "mcp", "agent", "unknown"]) {
      assert.equal(isDestinationVisible({ type }, types, true), true, type);
    }
  });

  it("hides MCP destinations when they are hidden", () => {
    assert.equal(isDestinationVisible({ type: "mcp" }, types, false), false);
    assert.equal(isDestinationVisible({ type: "agent" }, types, false), false);
    assert.equal(isDestinationVisible({ type: "webhook" }, types, false), true);
  });

  it("hides mcp destinations whose type the API no longer lists", () => {
    assert.equal(isDestinationVisible({ type: "mcp" }, {}, false), false);
    assert.equal(
      isDestinationVisible({ type: "mcp" }, undefined, false),
      false,
    );
  });

  it("keeps unknown non-MCP types, which render as unknown", () => {
    assert.equal(isDestinationVisible({ type: "unknown" }, types, false), true);
    assert.equal(
      isDestinationVisible({ type: "constructor" }, types, false),
      true,
    );
  });
});

describe("isExpired and destinationStatus", () => {
  const now = Date.parse("2026-10-09T12:00:00Z");

  it("never expires without a valid expires_at", () => {
    assert.equal(isExpired(undefined, now), false);
    assert.equal(isExpired(null, now), false);
    assert.equal(isExpired("", now), false);
    assert.equal(isExpired("not a date", now), false);
  });

  it("expires at and after expires_at", () => {
    assert.equal(isExpired("2026-10-09T11:59:59Z", now), true);
    assert.equal(isExpired("2026-10-09T12:00:00Z", now), true);
    assert.equal(isExpired("2026-10-09T12:00:00.001Z", now), false);
    assert.equal(isExpired("2026-10-10T00:00:00Z", now), false);
  });

  it("puts expiry before disabled", () => {
    assert.equal(
      destinationStatus(
        {
          disabled_at: "2026-10-01T00:00:00Z",
          expires_at: "2026-10-02T00:00:00Z",
        },
        now,
      ),
      "expired",
    );
    assert.equal(
      destinationStatus(
        {
          disabled_at: "2026-10-01T00:00:00Z",
          expires_at: "2026-11-01T00:00:00Z",
        },
        now,
      ),
      "disabled",
    );
    assert.equal(
      destinationStatus({ disabled_at: "", expires_at: null }, now),
      "active",
    );
    assert.equal(
      destinationStatus({ disabled_at: null as unknown as string }, now),
      "active",
    );
  });
});

describe("configString", () => {
  it("reads string values only", () => {
    const destination = {
      config: { principal: "user_1", count: 3, nested: { a: 1 } },
    };
    assert.equal(configString(destination, "principal"), "user_1");
    assert.equal(configString(destination, "count"), undefined);
    assert.equal(configString(destination, "nested"), undefined);
    assert.equal(configString(destination, "missing"), undefined);
  });

  it("never reads Object.prototype members", () => {
    assert.equal(configString({ config: {} }, "constructor"), undefined);
    assert.equal(configString({ config: {} }, "toString"), undefined);
  });

  it("handles a missing config", () => {
    assert.equal(
      configString({ config: null as unknown as Record<string, any> }, "url"),
      undefined,
    );
    assert.equal(
      configString(
        { config: undefined as unknown as Record<string, any> },
        "url",
      ),
      undefined,
    );
  });
});

describe("parseArguments", () => {
  it("returns null when there are no arguments", () => {
    assert.equal(parseArguments(undefined), null);
    assert.equal(parseArguments(null), null);
    assert.equal(parseArguments(""), null);
  });

  it("decodes the JSON string", () => {
    assert.deepEqual(
      parseArguments('{"currency":"USD","total":{"$gte":100}}'),
      { ok: true, value: { currency: "USD", total: { $gte: 100 } } },
    );
    assert.deepEqual(parseArguments("{}"), { ok: true, value: {} });
  });

  it("keeps the raw string when it isn't JSON", () => {
    assert.deepEqual(parseArguments("{not json"), {
      ok: false,
      raw: "{not json",
    });
  });

  it("passes an already decoded value through", () => {
    assert.deepEqual(parseArguments({ a: 1 }), { ok: true, value: { a: 1 } });
  });

  it("doesn't pollute Object.prototype", () => {
    const parsed = parseArguments('{"__proto__":{"polluted":true}}');
    assert.ok(parsed?.ok);
    assert.equal(({} as Record<string, unknown>).polluted, undefined);
  });
});
