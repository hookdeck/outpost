/// <reference types="bun-types" />
/*
 * Tests for registerMcpEvents against fakes of both MCP SDK majors, the real
 * @modelcontextprotocol/sdk 1.x (a dependency of this package) and, when it
 * is installed, the real @modelcontextprotocol/server v2.
 */

import { beforeAll, describe, expect, test } from "bun:test";
import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import * as z from "zod/v3";
import {
  JSONRPCError,
  MCPError,
  McpEventsApi,
  McpResult,
  registerMcpEvents,
} from "../mcp-events/index.js";

type Call = { method: string; tenantId: string; arg: unknown };

function fakeApi(answer: (call: Call) => unknown = (c) => ({ handledBy: c.method })) {
  const calls: Call[] = [];
  const record = (method: string) => async (tenantId: string, arg: unknown): Promise<McpResult> => {
    const call = { method, tenantId, arg };
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

async function rejection(promise: Promise<unknown>): Promise<unknown> {
  try {
    await promise;
  } catch (err) {
    return err;
  }
  throw new Error("expected the promise to reject");
}

type V2Handler = (params: unknown, ctx: unknown) => Promise<unknown>;

/** Mimics @modelcontextprotocol/server v2's Server surface. */
class FakeV2Server {
  capabilities: Record<string, unknown> = {};
  handlers = new Map<string, { schemas: { params: any }; handler: V2Handler }>();
  connected = false;
  registerCapabilities(caps: Record<string, unknown>) {
    if (this.connected) throw new Error("Cannot register capabilities after connecting to transport");
    this.capabilities = { ...this.capabilities, ...caps };
  }
  setRequestHandler(method: string, schemas: { params: any }, handler: V2Handler) {
    if (typeof schemas !== "object" || typeof handler !== "function") throw new TypeError("v2 signature");
    this.handlers.set(method, { schemas, handler });
  }
  async call(method: string, params: unknown, ctx: unknown) {
    const entry = this.handlers.get(method)!;
    const parsed = await entry.schemas.params["~standard"].validate(params);
    if (parsed.issues) throw new Error("invalid params");
    return entry.handler(parsed.value, ctx);
  }
}

const v2Ctx = (sub: string | undefined, signal?: AbortSignal) => ({
  mcpReq: { id: 1, method: "x", signal: signal ?? new AbortController().signal },
  http: sub === undefined ? undefined : { authInfo: { token: "t", clientId: "chatgpt", scopes: [], extra: { sub } } },
});

const principalFromV2 = (ctx: any) => ctx.http?.authInfo?.extra?.sub as string | undefined;
const principalFromV1 = (extra: any) => extra.authInfo?.extra?.sub as string | undefined;

describe("registerMcpEvents with the v2 API", () => {
  test("declares the events capability and registers the three methods", () => {
    const server = new FakeV2Server();
    registerMcpEvents(fakeApi().api, server, { resolveTenant: () => "t", resolvePrincipal: principalFromV2 });
    expect(server.capabilities).toEqual({ events: {}, extensions: { "io.modelcontextprotocol/events": {} } });
    expect([...server.handlers.keys()]).toEqual(["events/list", "events/subscribe", "events/unsubscribe"]);
  });

  test("accepts an McpServer through .server", () => {
    const server = new FakeV2Server();
    registerMcpEvents(fakeApi().api, { server }, { resolveTenant: () => "t", resolvePrincipal: principalFromV2 });
    expect(server.handlers.size).toBe(3);
  });

  test("the params schema accepts any object and nothing else", async () => {
    const server = new FakeV2Server();
    registerMcpEvents(fakeApi().api, server, { resolveTenant: () => "t", resolvePrincipal: principalFromV2 });
    const schema = server.handlers.get("events/subscribe")!.schemas.params["~standard"];
    expect(schema.version).toBe(1);
    expect(await schema.validate({ a: 1 })).toEqual({ value: { a: 1 } });
    expect((await schema.validate([])).issues).toHaveLength(1);
    expect((await schema.validate("x")).issues).toHaveLength(1);
  });

  test("resolves the principal and tenant from the request context", async () => {
    const { api, calls } = fakeApi();
    const server = new FakeV2Server();
    const tenants: unknown[] = [];
    registerMcpEvents(api, server, {
      resolvePrincipal: principalFromV2,
      resolveTenant: (principal, ctx) => {
        tenants.push([principal, principalFromV2(ctx)]);
        return `store_${principal}`;
      },
    });
    const params = { name: "order.created", delivery: { url: "https://x", secret: "whsec_a" } };

    expect(await server.call("events/subscribe", params, v2Ctx("user_1"))).toEqual({ handledBy: "subscribe" });
    expect(calls).toEqual([
      { method: "subscribe", tenantId: "store_user_1", arg: { principal: "user_1", params, allowed_topics: undefined } },
    ]);
    expect(tenants).toEqual([["user_1", "user_1"]]);
  });

  test("an unauthenticated request is Forbidden", async () => {
    const { api, calls } = fakeApi();
    const server = new FakeV2Server();
    registerMcpEvents(api, server, { resolveTenant: () => "t", resolvePrincipal: principalFromV2, codes: "sep-3415" });
    for (const method of ["events/list", "events/subscribe", "events/unsubscribe"]) {
      const err = await rejection(server.call(method, {}, v2Ctx(undefined)));
      expect(err).toBeInstanceOf(JSONRPCError);
      expect((err as JSONRPCError).code).toBe(-32024);
    }
    expect(calls).toHaveLength(0);
  });

  test("a failing principal resolver is an internal error reported to onError", async () => {
    const errors: unknown[] = [];
    const server = new FakeV2Server();
    registerMcpEvents(fakeApi().api, server, {
      resolveTenant: () => "t",
      resolvePrincipal: () => {
        throw new Error("token store down");
      },
      onError: (err, info) => errors.push([(err as Error).message, info]),
    });
    const err = await rejection(server.call("events/list", {}, v2Ctx("u")));
    expect((err as JSONRPCError).code).toBe(-32603);
    expect((err as JSONRPCError).message).toBe("Internal error");
    expect(errors).toEqual([["token store down", { method: "events/list" }]]);
  });

  test("passes the request's abort signal to the Outpost call", async () => {
    const seen: unknown[] = [];
    const api: McpEventsApi = {
      listEvents: async (_t, _q, options) => (seen.push(options?.signal), { events: [] }),
      subscribe: async () => ({}),
      unsubscribe: async () => ({}),
    };
    const server = new FakeV2Server();
    registerMcpEvents(api, server, { resolveTenant: () => "t", resolvePrincipal: principalFromV2 });
    const controller = new AbortController();
    await server.call("events/list", {}, v2Ctx("u", controller.signal));
    expect(seen[0]).toBe(controller.signal);
  });

  test("throws once connected, before registering any handler", () => {
    const server = new FakeV2Server();
    server.connected = true;
    expect(() => registerMcpEvents(fakeApi().api, server, { resolveTenant: () => "t", resolvePrincipal: principalFromV2 })).toThrow(
      "after connecting",
    );
    expect(server.handlers.size).toBe(0);
  });

  test("requires resolvePrincipal and an MCP server", () => {
    const server = new FakeV2Server();
    expect(() => registerMcpEvents(fakeApi().api, server, { resolveTenant: () => "t" } as never)).toThrow(TypeError);
    expect(() => registerMcpEvents(fakeApi().api, {} as never, { resolveTenant: () => "t", resolvePrincipal: () => "u" })).toThrow(
      TypeError,
    );
  });
});

/**
 * Connects a real 1.x client to `server`, sending `sub` as the token's
 * subject. `sent` collects the raw messages the server writes.
 */
async function connectV1(server: Server, sub: string | undefined, sent: unknown[] = []) {
  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  const send = clientTransport.send.bind(clientTransport);
  clientTransport.send = (message, options) =>
    send(message, {
      ...options,
      ...(sub === undefined ? {} : { authInfo: { token: "tok", clientId: "chatgpt", scopes: [], extra: { sub } } }),
    });
  const serverSend = serverTransport.send.bind(serverTransport);
  serverTransport.send = (message, options) => {
    sent.push(message);
    return serverSend(message, options);
  };
  await server.connect(serverTransport);
  const client = new Client({ name: "test", version: "1" });
  await client.connect(clientTransport);
  return client;
}

const anyResult = z.object({}).passthrough();

describe("registerMcpEvents with @modelcontextprotocol/sdk 1.x", () => {
  test("serves the events methods and advertises the capability", async () => {
    const { api, calls } = fakeApi(({ method }) =>
      method === "listEvents" ? { events: [{ name: "order.created" }], nextCursor: "n" } : { id: "sub_1", cursor: null });
    const server = new Server({ name: "s", version: "1" }, { capabilities: { tools: {} } });
    registerMcpEvents(api, server, {
      resolvePrincipal: principalFromV1,
      resolveTenant: (p) => `store_${p}`,
      allowedTopics: () => ["order.created"],
    });
    const sent: any[] = [];
    const client = await connectV1(server, "user_1", sent);

    // The 1.x client schema strips unknown capabilities, so read the wire.
    expect(sent[0].result.capabilities).toEqual({
      tools: {},
      events: {},
      extensions: { "io.modelcontextprotocol/events": {} },
    });

    const list = await client.request({ method: "events/list", params: { cursor: "c" } } as never, anyResult);
    expect(list).toEqual({ events: [{ name: "order.created" }], nextCursor: "n" });

    const params = { name: "order.created", arguments: { total: { $gte: 1 } }, delivery: { url: "https://x" } };
    expect(await client.request({ method: "events/subscribe", params } as never, anyResult)).toEqual({ id: "sub_1", cursor: null });
    await client.request({ method: "events/unsubscribe", params } as never, anyResult);

    expect(calls).toEqual([
      { method: "listEvents", tenantId: "store_user_1", arg: { cursor: "c", topics: ["order.created"] } },
      { method: "subscribe", tenantId: "store_user_1", arg: { principal: "user_1", params, allowed_topics: ["order.created"] } },
      { method: "unsubscribe", tenantId: "store_user_1", arg: { principal: "user_1", params } },
    ]);
    await client.close();
  });

  test("errors reach the client as JSON-RPC errors", async () => {
    const { api } = fakeApi(({ method }) => {
      if (method === "subscribe") {
        throw new MCPError({ kind: "resource_exhausted", code: -32013, message: "ResourceExhausted", data: { limit: "subscriptions", max: 100 } });
      }
      throw new Error("connect ECONNREFUSED 10.1.2.3:3333");
    });
    const server = new Server({ name: "s", version: "1" });
    registerMcpEvents(api, new McpServerShim(server), {
      resolvePrincipal: principalFromV1,
      resolveTenant: () => "t",
      allowedTopics: () => ["a"],
    });
    const client = await connectV1(server, "u");

    const exhausted = await rejection(client.request({ method: "events/subscribe", params: { name: "a" } } as never, anyResult));
    expect(exhausted).toMatchObject({ code: -32013, data: { limit: "subscriptions", max: 100 } });
    expect((exhausted as Error).message).toContain("ResourceExhausted");

    const hidden = await rejection(client.request({ method: "events/subscribe", params: { name: "b" } } as never, anyResult));
    expect(hidden).toMatchObject({ code: -32011, data: { kind: "event" } });

    const internal = await rejection(client.request({ method: "events/unsubscribe", params: {} } as never, anyResult));
    expect(internal).toMatchObject({ code: -32603 });
    expect((internal as Error).message).not.toContain("10.1.2.3");
    await client.close();
  });

  test("a request without a principal is Forbidden", async () => {
    const { api, calls } = fakeApi();
    const server = new Server({ name: "s", version: "1" });
    registerMcpEvents(api, server, { resolvePrincipal: principalFromV1, resolveTenant: () => "t" });
    const client = await connectV1(server, undefined);
    const err = await rejection(client.request({ method: "events/subscribe", params: {} } as never, anyResult));
    expect(err).toMatchObject({ code: -32012 });
    expect(calls).toHaveLength(0);
    await client.close();
  });

  test("accepts an McpServer", async () => {
    const mcp = new McpServer({ name: "s", version: "1" });
    registerMcpEvents(fakeApi().api, mcp, { resolvePrincipal: principalFromV1, resolveTenant: () => "t" });
    const client = await connectV1(mcp.server, "u");
    expect(await client.request({ method: "events/list", params: {} } as never, anyResult)).toEqual({ handledBy: "listEvents" });
    await client.close();
  });

  test("refuses to run after connect", async () => {
    const server = new Server({ name: "s", version: "1" });
    const client = await connectV1(server, "u");
    expect(() => registerMcpEvents(fakeApi().api, server, { resolvePrincipal: principalFromV1, resolveTenant: () => "t" })).toThrow();
    await client.close();
  });
});

/** An object exposing a 1.x Server as `.server`, like McpServer does. */
class McpServerShim {
  constructor(readonly server: Server) {}
}

// The v2 packages are not dependencies of this SDK; these tests run when they
// are installed (npm i --no-save @modelcontextprotocol/server @modelcontextprotocol/client).
// No top-level await: tshy also compiles this file as CommonJS.
function resolvable(name: string): boolean {
  try {
    Bun.resolveSync(name, process.cwd());
    return true;
  } catch {
    return false;
  }
}

const V2_SERVER = "@modelcontextprotocol/server";
const V2_CLIENT = "@modelcontextprotocol/client";

describe.skipIf(!resolvable(V2_SERVER) || !resolvable(V2_CLIENT))("registerMcpEvents with @modelcontextprotocol/server v2", () => {
  let v2Server: any;
  let v2Client: any;

  beforeAll(async () => {
    v2Server = await import(V2_SERVER);
    v2Client = await import(V2_CLIENT);
  });

  async function connect(
    sub: string | undefined,
    api: McpEventsApi,
    allowedTopics?: () => string[],
    mode: "auto" | "legacy" = "auto",
    bodies: string[] = [],
  ) {
    const handler = v2Server.createMcpHandler(() => {
      const server = new v2Server.McpServer({ name: "outpost-test", version: "1.0.0" });
      registerMcpEvents(api, server, {
        resolvePrincipal: principalFromV2,
        resolveTenant: (p) => `store_${p}`,
        allowedTopics,
      });
      return server;
    });
    const authInfo = sub === undefined ? undefined : { token: "tok", clientId: "chatgpt", scopes: [], extra: { sub } };
    const fetchLike = async (url: string | URL, init?: RequestInit) => {
      const response: Response = await handler.fetch(new Request(url, init), authInfo ? { authInfo } : {});
      bodies.push(await response.clone().text());
      return response;
    };
    const client = new v2Client.Client({ name: "test", version: "1" }, { versionNegotiation: { mode } });
    await client.connect(new v2Client.StreamableHTTPClientTransport(new URL("http://mcp.test/mcp"), { fetch: fetchLike }));
    return client;
  }

  test("serves the 2026-07-28 revision end to end", async () => {
    const { api, calls } = fakeApi(({ method }) =>
      method === "listEvents" ? { events: [{ name: "order.created" }] } : method === "subscribe" ? { id: "sub_1", refreshBefore: null, cursor: null, truncated: false } : {});
    const bodies: string[] = [];
    const client = await connect("user_1", api, () => ["order.created"], "auto", bodies);
    expect(client.getNegotiatedProtocolVersion()).toBe("2026-07-28");

    const discover = await client.request({ method: "server/discover", params: {} }, anyResult);
    expect(discover.capabilities.events).toEqual({});
    expect(discover.capabilities.extensions["io.modelcontextprotocol/events"]).toEqual({});

    bodies.length = 0;
    expect(await client.request({ method: "events/list", params: {} }, anyResult)).toMatchObject({ events: [{ name: "order.created" }] });
    // The SDK stamps the revision's required resultType on custom results.
    expect(bodies.join("")).toContain('"resultType":"complete"');
    const params = { name: "order.created", delivery: { mode: "webhook", url: "https://x", secret: "whsec_a" } };
    expect(await client.request({ method: "events/subscribe", params }, anyResult)).toMatchObject({ id: "sub_1", refreshBefore: null });
    expect(await client.request({ method: "events/unsubscribe", params }, anyResult)).toBeDefined();

    expect(calls.map((c) => [c.method, c.tenantId])).toEqual([
      ["listEvents", "store_user_1"],
      ["subscribe", "store_user_1"],
      ["unsubscribe", "store_user_1"],
    ]);
    expect((calls[1]!.arg as { params: unknown }).params).toEqual(params);
    await client.close();
  });

  test("errors keep their code and data", async () => {
    const { api } = fakeApi(() => {
      throw new MCPError({ kind: "callback_endpoint_error", code: -32015, message: "CallbackEndpointError", data: { reason: "timeout" } });
    });
    const client = await connect("u", api, () => ["a"]);
    const failed = await rejection(client.request({ method: "events/subscribe", params: { name: "a" } }, anyResult));
    expect(failed).toMatchObject({ code: -32015, data: { reason: "timeout" } });
    const hidden = await rejection(client.request({ method: "events/subscribe", params: { name: "b" } }, anyResult));
    expect(hidden).toMatchObject({ code: -32011, data: { kind: "event" } });
    await client.close();
  });

  test("an unauthenticated request is Forbidden", async () => {
    const { api, calls } = fakeApi();
    const client = await connect(undefined, api);
    expect(await rejection(client.request({ method: "events/list", params: {} }, anyResult))).toMatchObject({ code: -32012 });
    expect(calls).toHaveLength(0);
    await client.close();
  });

  test("also serves 2025-era clients through the legacy fallback", async () => {
    const { api, calls } = fakeApi(() => ({ events: [] }));
    const client = await connect("u", api, undefined, "legacy");
    expect(client.getNegotiatedProtocolVersion()).not.toBe("2026-07-28");
    expect(await client.request({ method: "events/list", params: {} }, anyResult)).toMatchObject({ events: [] });
    expect(calls).toHaveLength(1);
    await client.close();
  });
});
