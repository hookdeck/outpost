/// <reference types="bun-types" />
/*
 * Tests for the hand-written MCP Events helpers (src/mcp-events). Run with
 * `bun test ./__tests__`.
 */

import { afterAll, beforeAll, describe, expect, test } from "bun:test";
import { Outpost } from "../../src/index.js";
import {
  createMcpEventsClient,
  createMcpEventsHandlers,
  JSONRPCError,
  MCPError,
  McpEventsApi,
  McpEventsClient,
  McpEventsRequestError,
  mcpErrorFrom,
  McpResult,
} from "../../src/mcp-events/index.js";

type Recorded = { method: string; url: URL; headers: Headers; body: string };

/** An HTTPClient stub answering every request with `respond`. */
function stubHTTP(respond: (req: Request) => Response | Promise<Response>) {
  const calls: Recorded[] = [];
  return {
    calls,
    httpClient: {
      async request(req: Request): Promise<Response> {
        calls.push({
          method: req.method,
          url: new URL(req.url),
          headers: req.headers,
          body: await req.text(),
        });
        return respond(req);
      },
    },
  };
}

const json = (status: number, body: unknown) =>
  new Response(typeof body === "string" ? body : JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });

async function rejection(promise: Promise<unknown>): Promise<unknown> {
  try {
    await promise;
  } catch (err) {
    return err;
  }
  throw new Error("expected the promise to reject");
}

const MCP_ERROR = {
  kind: "callback_endpoint_error",
  code: -32015,
  message: "CallbackEndpointError",
  data: { reason: "challenge_failed" },
};

describe("McpEventsClient", () => {
  test("listEvents sends cursor, topics and limit and returns the result verbatim", async () => {
    const result = { events: [{ name: "order.created", inputSchema: { type: "object" } }], nextCursor: "dDpi" };
    const { calls, httpClient } = stubHTTP(() => json(200, result));
    const client = new McpEventsClient({ serverURL: "https://outpost.example.com/api/v2", apiKey: "key", httpClient });

    const got = await client.listEvents("store/1?x#y", { cursor: "abc", topics: ["a", "b"], limit: 5 });

    expect(got).toEqual(result);
    const call = calls[0]!;
    expect(call.method).toBe("GET");
    expect(call.url.pathname).toBe("/api/v2/tenants/store%2F1%3Fx%23y/mcp/events");
    expect(call.url.searchParams.get("cursor")).toBe("abc");
    expect(call.url.searchParams.get("topics")).toBe("a,b");
    expect(call.url.searchParams.get("limit")).toBe("5");
    expect(call.headers.get("authorization")).toBe("Bearer key");
    expect(call.headers.get("accept")).toBe("application/json");
  });

  test("a client-chosen cursor can't inject query parameters", async () => {
    const { calls, httpClient } = stubHTTP(() => json(200, { events: [] }));
    const client = new McpEventsClient({ serverURL: "http://h/api/v2", httpClient });
    await client.listEvents("t", { cursor: "x&topics=secret#frag", topics: ["a"] });
    expect(calls[0]!.url.searchParams.getAll("topics")).toEqual(["a"]);
    expect(calls[0]!.url.searchParams.get("cursor")).toBe("x&topics=secret#frag");
  });

  test("an empty topics list is sent as a present but empty param", async () => {
    const { calls, httpClient } = stubHTTP(() => json(200, { events: [] }));
    const client = new McpEventsClient({ serverURL: "http://localhost:3333/api/v2", httpClient });

    await client.listEvents("t", { topics: [] });
    await client.listEvents("t");

    expect(calls[0]!.url.search).toBe("?topics=");
    expect(calls[1]!.url.search).toBe("");
    expect(calls[1]!.headers.get("authorization")).toBeNull();
  });

  test("an /api/v1 base URL is rewritten to /api/v2", async () => {
    const { calls, httpClient } = stubHTTP(() => json(200, {}));
    for (const serverURL of ["http://h/api/v1", "http://h/api/v1/", "http://h/prefix/api/v1?q=1#f"]) {
      const client = new McpEventsClient({ serverURL, httpClient });
      await client.unsubscribe("t", { principal: "p", params: {} });
    }
    expect(calls.map((c) => c.url.href)).toEqual([
      "http://h/api/v2/tenants/t/mcp/subscriptions/unsubscribe",
      "http://h/api/v2/tenants/t/mcp/subscriptions/unsubscribe",
      "http://h/prefix/api/v2/tenants/t/mcp/subscriptions/unsubscribe",
    ]);
  });

  test("dot-segment and empty tenant IDs are rejected before any request", async () => {
    const { calls, httpClient } = stubHTTP(() => json(200, {}));
    const client = new McpEventsClient({ serverURL: "http://h/api/v2", httpClient });
    for (const tenant of ["", ".", ".."]) {
      expect(await rejection(client.listEvents(tenant))).toBeInstanceOf(TypeError);
    }
    // Encoded dots and slashes stay inside the tenant segment.
    await client.listEvents("../..%2e/admin");
    expect(calls).toHaveLength(1);
    expect(calls[0]!.url.pathname).toBe("/api/v2/tenants/..%2F..%252e%2Fadmin/mcp/events");
  });

  test("subscribe PUTs params untouched and forwards the result verbatim", async () => {
    const result = {
      id: "sub_1",
      refreshBefore: "2026-10-09T18:00:00Z",
      cursor: null,
      truncated: false,
      deliveryStatus: { active: true, lastDeliveryAt: null, lastError: null },
      futureField: { nested: [1, 2] },
    };
    const { calls, httpClient } = stubHTTP(() => json(200, result));
    const client = new McpEventsClient({ serverURL: "http://h/api/v2", apiKey: async () => "Bearer already", httpClient });
    const params = {
      name: "order.created",
      arguments: { total: { $gte: 100 } },
      delivery: { mode: "webhook", url: "https://r.example/cb", secret: "whsec_x" },
      cursor: null,
      _meta: { unknown: true },
    };

    const got = await client.subscribe("t", { principal: "user_1", params, allowed_topics: ["order.created"] });

    expect(got).toEqual(result);
    expect(calls[0]!.method).toBe("PUT");
    expect(calls[0]!.url.pathname).toBe("/api/v2/tenants/t/mcp/subscriptions");
    expect(calls[0]!.headers.get("authorization")).toBe("Bearer already");
    expect(calls[0]!.headers.get("content-type")).toBe("application/json");
    expect(JSON.parse(calls[0]!.body)).toEqual({ principal: "user_1", params, allowed_topics: ["order.created"] });
  });

  test("a 422 mcp_error rejects with MCPError", async () => {
    const { httpClient } = stubHTTP(() => json(422, { mcp_error: MCP_ERROR }));
    const client = new McpEventsClient({ serverURL: "http://h/api/v2", httpClient });

    const err = await rejection(client.subscribe("t", { principal: "p", params: {} }));

    expect(err).toBeInstanceOf(MCPError);
    const e = err as MCPError;
    expect(e.statusCode).toBe(422);
    expect(e.kind).toBe("callback_endpoint_error");
    expect(e.code).toBe(-32015);
    expect(e.message).toBe("CallbackEndpointError");
    expect(e.data).toEqual({ reason: "challenge_failed" });
    expect(e.mcpError).toEqual(MCP_ERROR);
  });

  test("other failures reject with McpEventsRequestError and a truncated body", async () => {
    const cases: Array<[number, unknown]> = [
      [422, { message: "validation error", data: ["x"] }],
      [422, { mcp_error: { kind: "x", code: "not-a-number", message: "m" } }],
      [500, "x".repeat(5000)],
      [503, ""],
      [200, "[]"],
      [200, "not json"],
      [200, ""],
      [200, "null"],
    ];
    for (const [status, body] of cases) {
      const { httpClient } = stubHTTP(() => json(status, body));
      const client = new McpEventsClient({ serverURL: "http://h/api/v2", httpClient });
      const err = await rejection(client.listEvents("t"));
      expect(err).toBeInstanceOf(McpEventsRequestError);
      expect((err as McpEventsRequestError).statusCode).toBe(status);
      expect(((err as McpEventsRequestError).body ?? "").length).toBeLessThanOrEqual(1024);
    }
  });

  test("an oversized response is rejected from Content-Length", async () => {
    const { httpClient } = stubHTTP(() => new Response("{}", { status: 200, headers: { "content-length": "999999999" } }));
    const client = new McpEventsClient({ serverURL: "http://h/api/v2", httpClient, maxResponseBytes: 1024 });
    const err = await rejection(client.listEvents("t"));
    expect(err).toBeInstanceOf(McpEventsRequestError);
    expect((err as Error).message).toContain("too large");
  });

  test("a response reached through a redirect is rejected", async () => {
    // An HTTP client that follows redirects despite `redirect: "error"`.
    const { httpClient } = stubHTTP(() => Object.defineProperty(json(200, { events: [] }), "redirected", { value: true }));
    const client = new McpEventsClient({ serverURL: "http://h/api/v2", httpClient });
    const err = await rejection(client.listEvents("t"));
    expect(err).toBeInstanceOf(McpEventsRequestError);
    expect((err as Error).message).toContain("redirected");
  });

  test("a network failure rejects with McpEventsRequestError", async () => {
    const client = new McpEventsClient({
      serverURL: "http://h/api/v2",
      httpClient: { request: () => Promise.reject(new TypeError("fetch failed")) },
    });
    const err = await rejection(client.listEvents("t"));
    expect(err).toBeInstanceOf(McpEventsRequestError);
    expect((err as McpEventsRequestError).statusCode).toBeUndefined();
  });

  test("a caller abort signal cancels the request", async () => {
    let seen: AbortSignal | undefined;
    const client = new McpEventsClient({
      serverURL: "http://h/api/v2",
      httpClient: {
        request: (req) => {
          seen = req.signal;
          return new Promise((_, reject) => req.signal.addEventListener("abort", () => reject(req.signal.reason)));
        },
      },
    });
    const controller = new AbortController();
    const pending = rejection(client.listEvents("t", {}, { signal: controller.signal }));
    controller.abort(new Error("cancelled"));
    expect(await pending).toBeInstanceOf(McpEventsRequestError);
    expect(seen?.aborted).toBe(true);
  });

  test("an already aborted signal never waits for the server", async () => {
    const client = new McpEventsClient({
      serverURL: "http://h/api/v2",
      httpClient: { request: (req) => (req.signal.aborted ? Promise.reject(req.signal.reason) : Promise.resolve(json(200, {}))) },
    });
    const controller = new AbortController();
    controller.abort();
    expect(await rejection(client.listEvents("t", {}, { signal: controller.signal }))).toBeInstanceOf(McpEventsRequestError);
  });

  test("rejects non-http server URLs and drops userinfo", () => {
    expect(() => new McpEventsClient({ serverURL: "file:///etc/passwd" })).toThrow(TypeError);
    expect(() => new McpEventsClient({ serverURL: "/api/v2" })).toThrow(TypeError);
    expect(new McpEventsClient({ serverURL: "http://user:pass@h:3333/api/v1?x" }).baseURL).toBe("http://h:3333/api/v2/");
  });
});

describe("McpEventsClient over HTTP", () => {
  let server: ReturnType<typeof Bun.serve>;
  let base: string;
  let redirectTargetHits = 0;

  beforeAll(() => {
    server = Bun.serve({
      port: 0,
      hostname: "127.0.0.1",
      async fetch(req) {
        const url = new URL(req.url);
        if (url.pathname.endsWith("/redirect-target")) {
          redirectTargetHits++;
          return json(200, { leaked: req.headers.get("authorization") });
        }
        const tenant = url.pathname.split("/")[4];
        switch (tenant) {
          case "big":
            // Streamed, with no Content-Length.
            return new Response(
              new ReadableStream({
                start(controller) {
                  const chunk = new TextEncoder().encode("x".repeat(64 * 1024));
                  for (let i = 0; i < 64; i++) controller.enqueue(chunk);
                  controller.close();
                },
              }),
              { status: 200 },
            );
          case "slow":
            await Bun.sleep(2000);
            return json(200, {});
          case "redirect":
            return new Response(null, { status: 302, headers: { location: `${base}/redirect-target` } });
          default:
            return json(200, { events: [], echo: req.headers.get("authorization") });
        }
      },
    });
    base = `http://127.0.0.1:${server.port}/api/v2`;
  });

  afterAll(() => {
    server.stop(true);
  });

  test("uses the global fetch by default", async () => {
    const client = new McpEventsClient({ serverURL: base, apiKey: "k" });
    expect(await client.listEvents("ok")).toEqual({ events: [], echo: "Bearer k" });
  });

  test("a streamed body over the limit is cut off", async () => {
    const client = new McpEventsClient({ serverURL: base, maxResponseBytes: 256 * 1024 });
    const err = await rejection(client.listEvents("big"));
    expect(err).toBeInstanceOf(McpEventsRequestError);
    expect((err as Error).message).toContain("too large");
  });

  test("times out", async () => {
    const client = new McpEventsClient({ serverURL: base, timeoutMs: 100 });
    const started = Date.now();
    expect(await rejection(client.listEvents("slow"))).toBeInstanceOf(McpEventsRequestError);
    expect(Date.now() - started).toBeLessThan(1500);
  });

  test("does not follow redirects", async () => {
    const client = new McpEventsClient({ serverURL: base, apiKey: "secret-key" });
    expect(await rejection(client.listEvents("redirect"))).toBeInstanceOf(McpEventsRequestError);
    expect(redirectTargetHits).toBe(0);
  });
});

describe("createMcpEventsClient", () => {
  test("reads server URL, API key and HTTP client from an Outpost instance", async () => {
    const { calls, httpClient } = stubHTTP(() => json(200, { events: [] }));
    // The SDK's HTTPClient only needs request(); a stub stands in for it.
    const outpost = new Outpost({ serverURL: "http://localhost:3333/api/v1", apiKey: "admin-key", httpClient: httpClient as never });

    const client = createMcpEventsClient(outpost);
    await client.listEvents("t");

    expect(calls[0]!.url.href).toBe("http://localhost:3333/api/v2/tenants/t/mcp/events");
    expect(calls[0]!.headers.get("authorization")).toBe("Bearer admin-key");
  });

  test("returns an existing client as is and rejects anything else", () => {
    const api: McpEventsApi = {
      listEvents: async () => ({}),
      subscribe: async () => ({}),
      unsubscribe: async () => ({}),
    };
    expect(createMcpEventsClient(api)).toBe(api);
    expect(() => createMcpEventsClient({} as never)).toThrow(TypeError);
    expect(() => createMcpEventsClient(null as never)).toThrow(TypeError);
  });
});

describe("mcpErrorFrom", () => {
  test("recognises mcp_error by duck typing", () => {
    expect(mcpErrorFrom(new MCPError(MCP_ERROR))).toEqual(MCP_ERROR);
    expect(mcpErrorFrom({ mcpError: MCP_ERROR })).toEqual(MCP_ERROR);
    expect(mcpErrorFrom({ mcp_error: MCP_ERROR })).toEqual(MCP_ERROR);
    expect(mcpErrorFrom({ statusCode: 422, body: JSON.stringify({ mcp_error: MCP_ERROR }) })).toEqual(MCP_ERROR);
  });

  test("ignores everything else", () => {
    expect(mcpErrorFrom(undefined)).toBeUndefined();
    expect(mcpErrorFrom("mcp_error")).toBeUndefined();
    expect(mcpErrorFrom(new Error("x"))).toBeUndefined();
    expect(mcpErrorFrom({ mcp_error: { code: 1.5, message: "m" } })).toBeUndefined();
    expect(mcpErrorFrom({ mcp_error: { code: -1 } })).toBeUndefined();
    expect(mcpErrorFrom({ statusCode: 500, body: JSON.stringify({ mcp_error: MCP_ERROR }) })).toBeUndefined();
    expect(mcpErrorFrom({ statusCode: 422, body: "{" })).toBeUndefined();
    expect(mcpErrorFrom({ statusCode: 422, body: " ".repeat(70 * 1024) })).toBeUndefined();
  });
});

type FakeCall = { method: keyof McpEventsApi; tenantId: string; arg: unknown; signal: AbortSignal | undefined };

/** A fake MCP Events client recording calls and answering with `answer`. */
function fakeApi(answer: (call: FakeCall) => unknown = () => ({ ok: true })) {
  const calls: FakeCall[] = [];
  const record = (method: keyof McpEventsApi) =>
    async (tenantId: string, arg: unknown, options?: { signal?: AbortSignal | undefined }): Promise<McpResult> => {
      const call = { method, tenantId, arg, signal: options?.signal };
      calls.push(call);
      return answer(call) as McpResult;
    };
  const api: McpEventsApi = {
    listEvents: record("listEvents"),
    subscribe: record("subscribe"),
    unsubscribe: record("unsubscribe"),
  };
  return { api, calls };
}

function expectRPC(err: unknown, code: number, message: string, data?: unknown) {
  expect(err).toBeInstanceOf(JSONRPCError);
  const e = err as JSONRPCError;
  expect(e.code).toBe(code);
  expect(e.message).toBe(message);
  expect(e.data).toEqual(data);
}

describe("createMcpEventsHandlers", () => {
  const tenantOf = (principal: string) => `tenant_of_${principal}`;

  test("requires resolveTenant", () => {
    expect(() => createMcpEventsHandlers(fakeApi().api, {} as never)).toThrow(TypeError);
  });

  test("forwards each method to its endpoint with the resolved tenant", async () => {
    const { api, calls } = fakeApi(({ method }) => ({ from: method }));
    const h = createMcpEventsHandlers(api, { resolveTenant: tenantOf });
    const params = { name: "order.created", arguments: {}, delivery: { url: "https://x" } };

    expect(await h.handleList("u1", { cursor: "c1" })).toEqual({ from: "listEvents" });
    expect(await h.handleSubscribe("u1", params)).toEqual({ from: "subscribe" });
    expect(await h.handleUnsubscribe("u1", params)).toEqual({ from: "unsubscribe" });

    expect(calls).toEqual([
      { method: "listEvents", tenantId: "tenant_of_u1", arg: { cursor: "c1", topics: undefined }, signal: undefined },
      { method: "subscribe", tenantId: "tenant_of_u1", arg: { principal: "u1", params, allowed_topics: undefined }, signal: undefined },
      { method: "unsubscribe", tenantId: "tenant_of_u1", arg: { principal: "u1", params }, signal: undefined },
    ]);
  });

  test("a missing principal is Forbidden for every method, without calling Outpost", async () => {
    const { api, calls } = fakeApi();
    let resolved = 0;
    const h = createMcpEventsHandlers(api, { resolveTenant: () => (resolved++, "t") });
    for (const principal of [undefined, null, "", 42 as never]) {
      expectRPC(await rejection(h.handleList(principal, {})), -32012, "Forbidden");
      expectRPC(await rejection(h.handleSubscribe(principal, { name: "x" })), -32012, "Forbidden");
      expectRPC(await rejection(h.handleUnsubscribe(principal, { name: "x" })), -32012, "Forbidden");
    }
    expect(calls).toHaveLength(0);
    expect(resolved).toBe(0);
  });

  test("the sep-3415 profile changes the local codes", async () => {
    const h = createMcpEventsHandlers(fakeApi().api, {
      resolveTenant: tenantOf,
      allowedTopics: () => ["a"],
      codes: "sep-3415",
    });
    expectRPC(await rejection(h.handleSubscribe(undefined, {})), -32024, "Forbidden");
    expectRPC(await rejection(h.handleSubscribe("u", { name: "b" })), -32023, "NotFound", { kind: "event" });
  });

  test("custom codes are accepted", async () => {
    const h = createMcpEventsHandlers(fakeApi().api, {
      resolveTenant: tenantOf,
      codes: { invalidParams: 1, notFound: 2, forbidden: 3, resourceExhausted: 4, unsupported: 5, callbackEndpointError: 6, internalError: 7 },
    });
    expectRPC(await rejection(h.handleList("", {})), 3, "Forbidden");
    expect(() => createMcpEventsHandlers(fakeApi().api, { resolveTenant: tenantOf, codes: "nope" as never })).toThrow(TypeError);
  });

  test("a principal without a tenant is Forbidden", async () => {
    const { api, calls } = fakeApi();
    const seen: unknown[] = [];
    for (const tenant of [null, undefined, "", 7]) {
      const h = createMcpEventsHandlers<{ req: number }>(api, {
        resolveTenant: (principal, context) => {
          seen.push([principal, context]);
          return tenant as never;
        },
      });
      expectRPC(await rejection(h.handleList("u", {}, { context: { req: 1 } })), -32012, "Forbidden");
    }
    expect(calls).toHaveLength(0);
    expect(seen[0]).toEqual(["u", { req: 1 }]);
  });

  test("params must be an object; absent params are {}", async () => {
    const { api, calls } = fakeApi();
    const h = createMcpEventsHandlers(api, { resolveTenant: tenantOf });
    for (const params of [[], "x", 1, true]) {
      expectRPC(await rejection(h.handleSubscribe("u", params)), -32602, "InvalidParams", { field: "params", reason: "invalid" });
    }
    await h.handleUnsubscribe("u", undefined);
    await h.handleList("u", null);
    expect(calls.map((c) => c.arg)).toEqual([
      { principal: "u", params: {} },
      { cursor: undefined, topics: undefined },
    ]);
  });

  test("a non-string cursor is InvalidParams; null is no cursor", async () => {
    const { api, calls } = fakeApi();
    const h = createMcpEventsHandlers(api, { resolveTenant: tenantOf });
    for (const cursor of [1, {}, []]) {
      expectRPC(await rejection(h.handleList("u", { cursor })), -32602, "InvalidParams", { field: "cursor", reason: "invalid" });
    }
    await h.handleList("u", { cursor: null });
    expect(calls).toHaveLength(1);
    expect(calls[0]!.arg).toEqual({ cursor: undefined, topics: undefined });
  });

  test("allowedTopics narrows events/list, and an empty allowlist answers locally", async () => {
    const { api, calls } = fakeApi(() => ({ events: ["from outpost"] }));
    let allowed: string[] = ["order.created", "order.paid"];
    const h = createMcpEventsHandlers(api, { resolveTenant: tenantOf, allowedTopics: async () => allowed });

    expect(await h.handleList("u", {})).toEqual({ events: ["from outpost"] });
    expect(calls[0]!.arg).toEqual({ cursor: undefined, topics: ["order.created", "order.paid"] });

    allowed = [];
    expect(await h.handleList("u", {})).toEqual({ events: [] });
    // Entries that can't be topic names are dropped, never widened.
    allowed = ["", "a,b"];
    expect(await h.handleList("u", { cursor: "c" })).toEqual({ events: [] });
    expect(calls).toHaveLength(1);
  });

  test("allowedTopics rejects subscribing outside the allowlist with NotFound", async () => {
    const { api, calls } = fakeApi();
    const h = createMcpEventsHandlers(api, { resolveTenant: tenantOf, allowedTopics: () => ["order.created"] });

    expectRPC(await rejection(h.handleSubscribe("u", { name: "order.refunded" })), -32011, "NotFound", { kind: "event" });
    expect(calls).toHaveLength(0);

    await h.handleSubscribe("u", { name: "order.created" });
    expect(calls[0]!.arg).toEqual({ principal: "u", params: { name: "order.created" }, allowed_topics: ["order.created"] });
  });

  test("an empty allowlist rejects every subscribe", async () => {
    const { api, calls } = fakeApi();
    const h = createMcpEventsHandlers(api, { resolveTenant: tenantOf, allowedTopics: () => [] });
    expectRPC(await rejection(h.handleSubscribe("u", { name: "order.created" })), -32011, "NotFound", { kind: "event" });
    expect(calls).toHaveLength(0);
  });

  test("a non-string name is left to Outpost, with the allowlist attached", async () => {
    const { api, calls } = fakeApi();
    const h = createMcpEventsHandlers(api, { resolveTenant: tenantOf, allowedTopics: () => ["a"] });
    await h.handleSubscribe("u", { name: ["a"] });
    expect(calls[0]!.arg).toEqual({ principal: "u", params: { name: ["a"] }, allowed_topics: ["a"] });
  });

  test("an inherited name from a __proto__ member is not trusted", async () => {
    const { api, calls } = fakeApi();
    const h = createMcpEventsHandlers(api, { resolveTenant: tenantOf, allowedTopics: () => ["allowed"] });

    // JSON.parse creates an own "__proto__" member, which a spread or a
    // record parser may turn into the object's prototype.
    const params = Object.create(JSON.parse('{"name":"allowed"}')) as Record<string, unknown>;
    params["delivery"] = { url: "https://x" };
    await h.handleSubscribe("u", params);

    const sent = JSON.parse(JSON.stringify(calls[0]!.arg)) as Record<string, unknown>;
    // The serialized request has no name, and the allowlist goes with it.
    expect(sent).toEqual({ principal: "u", params: { delivery: { url: "https://x" } }, allowed_topics: ["allowed"] });

    const denied = Object.create(JSON.parse('{"name":"allowed"}')) as Record<string, unknown>;
    denied["name"] = "secret.topic";
    expectRPC(await rejection(h.handleSubscribe("u", denied)), -32011, "NotFound", { kind: "event" });
  });

  test("a differently cased name member can't smuggle a hidden topic", async () => {
    const { api, calls } = fakeApi();
    const h = createMcpEventsHandlers(api, { resolveTenant: tenantOf, allowedTopics: () => ["allowed"] });
    // Go's JSON decoding (Outpost) matches member names case-insensitively.
    for (const params of [
      JSON.parse('{"name":"allowed","Name":"secret.topic"}'),
      JSON.parse('{"NAME":"secret.topic"}'),
      JSON.parse('{"nAmE":"secret.topic","name":"allowed"}'),
    ]) {
      expectRPC(await rejection(h.handleSubscribe("u", params)), -32011, "NotFound", { kind: "event" });
    }
    expect(calls).toHaveLength(0);
    await h.handleSubscribe("u", JSON.parse('{"name":"allowed","Name":"allowed"}'));
    expect(calls).toHaveLength(1);
  });

  test("unsubscribe is never filtered by the allowlist", async () => {
    const { api, calls } = fakeApi(() => ({}));
    let asked = 0;
    const h = createMcpEventsHandlers(api, { resolveTenant: tenantOf, allowedTopics: () => (asked++, []) });
    expect(await h.handleUnsubscribe("u", { name: "anything" })).toEqual({});
    expect(calls).toHaveLength(1);
    expect(asked).toBe(0);
  });

  test("allowedTopics must return an array", async () => {
    const errors: unknown[] = [];
    const h = createMcpEventsHandlers(fakeApi().api, {
      resolveTenant: tenantOf,
      allowedTopics: () => undefined as never,
      onError: (err) => errors.push(err),
    });
    expectRPC(await rejection(h.handleList("u", {})), -32603, "Internal error");
    expect(errors[0]).toBeInstanceOf(TypeError);
  });

  test("mcp_error from Outpost is rethrown verbatim as the JSON-RPC error", async () => {
    const shapes: unknown[] = [
      new MCPError(MCP_ERROR),
      { mcpError: MCP_ERROR },
      { mcp_error: MCP_ERROR },
      Object.assign(new Error("API error occurred"), { statusCode: 422, body: JSON.stringify({ mcp_error: MCP_ERROR }) }),
    ];
    for (const shape of shapes) {
      const { api } = fakeApi(() => {
        throw shape;
      });
      const h = createMcpEventsHandlers(api, { resolveTenant: tenantOf, codes: "sep-3415" });
      // Outpost's own code wins over the local profile.
      expectRPC(await rejection(h.handleSubscribe("u", {})), -32015, "CallbackEndpointError", { reason: "challenge_failed" });
    }
  });

  test("any other failure is a generic internal error that leaks nothing", async () => {
    const secretBody = '{"message":"boom","data":["whsec_supersecret"]}';
    const failures: unknown[] = [
      new McpEventsRequestError("Outpost responded with status 500", { statusCode: 500, body: secretBody }),
      new TypeError("fetch failed: connect ECONNREFUSED 10.0.0.1:3333"),
      { statusCode: 400, body: secretBody },
      { mcp_error: { code: "x", message: "m" } },
      "a string",
    ];
    for (const failure of failures) {
      const errors: Array<[unknown, unknown]> = [];
      const { api } = fakeApi(() => {
        throw failure;
      });
      const h = createMcpEventsHandlers(api, {
        resolveTenant: tenantOf,
        onError: (err, info) => {
          errors.push([err, info]);
          throw new Error("a broken logger");
        },
      });
      const err = await rejection(h.handleSubscribe("u", {}));
      expectRPC(err, -32603, "Internal error", undefined);
      expect(JSON.stringify({ ...(err as object), message: (err as Error).message })).not.toContain("whsec_");
      expect(errors).toEqual([[failure, { method: "events/subscribe" }]]);
    }
  });

  test("a result that is not an object is an internal error", async () => {
    for (const result of [null, [], "x", 1]) {
      const h = createMcpEventsHandlers(fakeApi(() => result).api, { resolveTenant: tenantOf });
      expectRPC(await rejection(h.handleList("u", {})), -32603, "Internal error");
    }
  });

  test("resolver failures are internal errors; a thrown JSONRPCError passes through", async () => {
    const h = createMcpEventsHandlers(fakeApi().api, {
      resolveTenant: (p) => {
        if (p === "custom") throw new JSONRPCError(-32012, "Forbidden", { reason: "suspended" });
        throw new Error("db down");
      },
    });
    expectRPC(await rejection(h.handleList("u", {})), -32603, "Internal error");
    expectRPC(await rejection(h.handleList("custom", {})), -32012, "Forbidden", { reason: "suspended" });
  });

  test("the abort signal reaches the client", async () => {
    const { api, calls } = fakeApi();
    const h = createMcpEventsHandlers(api, { resolveTenant: tenantOf });
    const controller = new AbortController();
    await h.handleUnsubscribe("u", {}, { signal: controller.signal });
    expect(calls[0]!.signal).toBe(controller.signal);
  });

  test("works end to end with the HTTP client", async () => {
    const { calls, httpClient } = stubHTTP((req) =>
      req.method === "PUT" ? json(422, { mcp_error: MCP_ERROR }) : json(200, { events: [{ name: "a" }] }));
    const h = createMcpEventsHandlers(
      { serverURL: "http://h/api/v2", apiKey: "k", httpClient },
      { resolveTenant: tenantOf, allowedTopics: () => ["a"] },
    );
    expect(await h.handleList("u", {})).toEqual({ events: [{ name: "a" }] });
    expectRPC(await rejection(h.handleSubscribe("u", { name: "a" })), -32015, "CallbackEndpointError", { reason: "challenge_failed" });
    expect(calls.map((c) => `${c.method} ${c.url.pathname}${c.url.search}`)).toEqual([
      "GET /api/v2/tenants/tenant_of_u/mcp/events?topics=a",
      "PUT /api/v2/tenants/tenant_of_u/mcp/subscriptions",
    ]);
  });
});
