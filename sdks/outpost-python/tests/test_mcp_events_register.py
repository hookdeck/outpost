"""Tests for outpost_sdk.mcp_events.register() against the real ``mcp`` package.

Skipped unless ``mcp`` version 2 or later is installed (it is not an SDK
dependency): pip install "mcp>=2". With OUTPOST_REQUIRE_MCP_SDKS=1 (set in CI)
they fail instead.
"""

from __future__ import annotations

import asyncio
import importlib.metadata
import os
from typing import Any, Optional

import pytest

if os.environ.get("OUTPOST_REQUIRE_MCP_SDKS") == "1":
    assert int(importlib.metadata.version("mcp").split(".")[0]) >= 2, "register() needs mcp 2 or later"
else:
    pytest.importorskip("mcp")
    if int(importlib.metadata.version("mcp").split(".")[0]) < 2:
        pytest.skip("register() needs mcp 2 or later", allow_module_level=True)

# pylint: disable=wrong-import-position
import mcp_types as types  # noqa: E402
from mcp import Client  # noqa: E402
from mcp.server import Server  # noqa: E402
from mcp.server.mcpserver import MCPServer  # noqa: E402
from mcp.shared.exceptions import MCPError as ProtocolError  # noqa: E402
from pydantic import TypeAdapter  # noqa: E402

from outpost_sdk.mcp_events import MCPError, register  # noqa: E402

ANY = TypeAdapter(dict)


class FakeAPI:
    def __init__(self, answer: Any = None) -> None:
        self.calls: list[tuple[str, str, Any]] = []
        self._answer = answer or (lambda method, tenant, arg: {"from": method})

    def _record(self, method: str, tenant_id: str, arg: Any) -> Any:
        self.calls.append((method, tenant_id, arg))
        return self._answer(method, tenant_id, arg)

    async def list_events(self, tenant_id: str, *, cursor: Optional[str] = None, topics: Any = None, limit: Any = None) -> Any:
        return self._record("list_events", tenant_id, {"cursor": cursor, "topics": topics})

    async def subscribe(self, tenant_id: str, body: Any) -> Any:
        return self._record("subscribe", tenant_id, body)

    async def unsubscribe(self, tenant_id: str, body: Any) -> Any:
        return self._record("unsubscribe", tenant_id, body)


async def request(client: Client, method: str, params: dict[str, Any]) -> dict[str, Any]:
    req = types.Request[dict[str, Any], str](method=method, params=params)
    return await client.session.send_request(req, ANY)  # type: ignore[arg-type]


def build(api: FakeAPI, principal: Optional[str], *, high_level: bool = False, **kwargs: Any) -> Any:
    server: Any = MCPServer("outpost-test") if high_level else Server("outpost-test")
    register(
        api,
        server,
        resolve_principal=lambda ctx: principal,
        resolve_tenant=lambda p, ctx: f"store_{p}",
        **kwargs,
    )
    return server


@pytest.mark.parametrize("mode", ["auto", "legacy"])
def test_serves_the_events_methods(mode: str) -> None:
    def answer(method: str, tenant: str, arg: Any) -> Any:
        if method == "list_events":
            return {"events": [{"name": "order.created"}], "nextCursor": "n"}
        if method == "subscribe":
            return {"id": "sub_1", "refreshBefore": None, "cursor": None, "truncated": False}
        return {}

    api = FakeAPI(answer)
    server = build(api, "user_1", allowed_topics=lambda p, ctx: ["order.created"])
    params = {"name": "order.created", "arguments": {"total": {"$gte": 1}}, "delivery": {"url": "https://x"}}

    async def go() -> None:
        async with Client(server, mode=mode) as client:
            listed = await request(client, "events/list", {"cursor": "c"})
            assert listed["events"] == [{"name": "order.created"}]
            assert listed["nextCursor"] == "n"
            subscribed = await request(client, "events/subscribe", params)
            assert subscribed["id"] == "sub_1"
            assert subscribed["refreshBefore"] is None
            if mode == "auto":
                assert client.protocol_version == "2026-07-28"
                # `extensions` is a 2026-07-28 field; legacy sessions drop it.
                assert client.server_capabilities.extensions == {"io.modelcontextprotocol/events": {}}
                # The SDK stamps the revision's required resultType.
                assert subscribed["resultType"] == "complete"
                discover = await request(client, "server/discover", {})
                assert discover["capabilities"]["events"] == {}
                assert discover["capabilities"]["extensions"] == {"io.modelcontextprotocol/events": {}}
            await request(client, "events/unsubscribe", params)

    asyncio.run(go())
    assert [(m, t) for m, t, _ in api.calls] == [
        ("list_events", "store_user_1"),
        ("subscribe", "store_user_1"),
        ("unsubscribe", "store_user_1"),
    ]
    assert api.calls[0][2] == {"cursor": "c", "topics": ["order.created"]}
    sent = api.calls[1][2]
    assert sent["principal"] == "user_1"
    assert sent["allowed_topics"] == ["order.created"]
    assert {k: v for k, v in sent["params"].items() if k != "_meta"} == params


def test_errors_reach_the_client() -> None:
    def answer(method: str, tenant: str, arg: Any) -> Any:
        if method == "subscribe":
            raise MCPError(
                {"kind": "resource_exhausted", "code": -32013, "message": "ResourceExhausted", "data": {"limit": "subscriptions"}}
            )
        raise ConnectionError("connect ECONNREFUSED 10.1.2.3:3333")

    server = build(FakeAPI(answer), "u", high_level=True, allowed_topics=lambda p, ctx: ["a"])

    async def go() -> None:
        async with Client(server) as client:
            with pytest.raises(ProtocolError) as exc:
                await request(client, "events/subscribe", {"name": "a"})
            assert (exc.value.code, exc.value.message, exc.value.data) == (
                -32013,
                "ResourceExhausted",
                {"limit": "subscriptions"},
            )
            with pytest.raises(ProtocolError) as exc:
                await request(client, "events/subscribe", {"name": "b"})
            assert (exc.value.code, exc.value.data) == (-32011, {"kind": "event"})
            with pytest.raises(ProtocolError) as exc:
                await request(client, "events/unsubscribe", {})
            assert exc.value.code == -32603
            assert "10.1.2.3" not in exc.value.message

    asyncio.run(go())


def test_missing_principal_is_forbidden() -> None:
    api = FakeAPI()
    server = build(api, None, codes="sep-3415")

    async def go() -> None:
        async with Client(server) as client:
            for method in ("events/list", "events/subscribe", "events/unsubscribe"):
                with pytest.raises(ProtocolError) as exc:
                    await request(client, method, {})
                assert exc.value.code == -32024

    asyncio.run(go())
    assert not api.calls


def test_failing_principal_resolver_is_internal() -> None:
    reported: list[Any] = []
    server = Server("outpost-test")

    def principal(ctx: Any) -> str:
        raise RuntimeError("token store down")

    register(
        FakeAPI(),
        server,
        resolve_principal=principal,
        resolve_tenant=lambda p, ctx: "t",
        on_error=lambda err, method: reported.append((str(err), method)),
    )

    async def go() -> None:
        async with Client(server) as client:
            with pytest.raises(ProtocolError) as exc:
                await request(client, "events/list", {})
            assert (exc.value.code, exc.value.message) == (-32603, "Internal error")

    asyncio.run(go())
    assert reported == [("token store down", "events/list")]


def test_registering_twice_adds_one_capability_middleware() -> None:
    server = Server("outpost-test")
    for _ in range(2):
        register(FakeAPI(), server, resolve_principal=lambda ctx: "u", resolve_tenant=lambda p, ctx: "t")
    marked = [m for m in server.middleware if getattr(m, "_outpost_events_capability", False)]
    assert len(marked) == 1
