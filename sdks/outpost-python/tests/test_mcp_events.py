"""Tests for the hand-written MCP Events helpers (outpost_sdk.mcp_events)."""

from __future__ import annotations

import asyncio
import json
from typing import Any, Optional

import httpx
import pytest

from outpost_sdk import Outpost
from outpost_sdk import mcp_events
from outpost_sdk.mcp_events import (
    ErrorCodes,
    JSONRPCError,
    MCPError,
    MCPEventsClient,
    MCPEventsHandlers,
    MCPEventsRequestError,
    create_client,
    mcp_error_from,
)

MCP_ERROR = {
    "kind": "callback_endpoint_error",
    "code": -32015,
    "message": "CallbackEndpointError",
    "data": {"reason": "challenge_failed"},
}


def run(coro: Any) -> Any:
    return asyncio.run(coro)


class Recorder:
    """An httpx MockTransport recording requests and answering with `respond`."""

    def __init__(self, respond: Any) -> None:
        self.requests: list[httpx.Request] = []
        self._respond = respond

    async def handle(self, request: httpx.Request) -> httpx.Response:
        await request.aread()
        self.requests.append(request)
        result = self._respond(request)
        if asyncio.iscoroutine(result):
            result = await result
        return result

    def client(self, **kwargs: Any) -> MCPEventsClient:
        kwargs.setdefault("server_url", "https://outpost.example.com/api/v2")
        return MCPEventsClient(async_client=httpx.AsyncClient(transport=httpx.MockTransport(self.handle)), **kwargs)


def json_response(status: int, body: Any) -> httpx.Response:
    return httpx.Response(status, content=body if isinstance(body, (str, bytes)) else json.dumps(body))


class TestClient:
    def test_list_events_sends_cursor_topics_and_limit(self) -> None:
        result = {"events": [{"name": "order.created", "inputSchema": {"type": "object"}}], "nextCursor": "dDpi"}
        rec = Recorder(lambda r: json_response(200, result))
        client = rec.client(api_key="key")

        got = run(client.list_events("store/1?x#y", cursor="abc", topics=["a", "b"], limit=5))

        assert got == result
        req = rec.requests[0]
        assert req.method == "GET"
        assert req.url.raw_path.decode() == "/api/v2/tenants/store%2F1%3Fx%23y/mcp/events?cursor=abc&topics=a%2Cb&limit=5"
        assert req.headers["authorization"] == "Bearer key"
        assert req.headers["accept"] == "application/json"
        assert req.headers["accept-encoding"] == "identity"

    def test_cursor_cannot_inject_query_parameters(self) -> None:
        rec = Recorder(lambda r: json_response(200, {"events": []}))
        run(rec.client().list_events("t", cursor="x&topics=secret#frag", topics=["a"]))
        params = rec.requests[0].url.params
        assert params.get_list("topics") == ["a"]
        assert params["cursor"] == "x&topics=secret#frag"

    def test_empty_topics_is_sent_as_present_but_empty(self) -> None:
        rec = Recorder(lambda r: json_response(200, {"events": []}))
        client = rec.client()
        run(client.list_events("t", topics=[]))
        run(client.list_events("t"))
        assert rec.requests[0].url.query == b"topics="
        assert rec.requests[1].url.query == b""
        assert "authorization" not in rec.requests[1].headers

    @pytest.mark.parametrize(
        "server_url,expected",
        [
            ("http://h/api/v1", "http://h/api/v2/"),
            ("http://h/api/v1/", "http://h/api/v2/"),
            ("http://h/prefix/api/v1?q=1#f", "http://h/prefix/api/v2/"),
            ("https://api.outpost.hookdeck.com/2025-07-01", "https://api.outpost.hookdeck.com/2025-07-01/"),
            ("http://user:pass@h:3333/api/v2", "http://h:3333/api/v2/"),
            ("http://[::1]:3333/api/v2", "http://[::1]:3333/api/v2/"),
        ],
    )
    def test_base_url_is_normalized_to_v2(self, server_url: str, expected: str) -> None:
        assert MCPEventsClient(server_url).base_url == expected

    @pytest.mark.parametrize("server_url", ["file:///etc/passwd", "localhost:3333", "/api/v2", "http://user@/api/v2"])
    def test_rejects_non_http_server_urls(self, server_url: str) -> None:
        with pytest.raises(ValueError):
            MCPEventsClient(server_url)

    def test_dot_segment_and_empty_tenant_ids_are_rejected(self) -> None:
        rec = Recorder(lambda r: json_response(200, {}))
        client = rec.client()
        for tenant in ["", ".", ".."]:
            with pytest.raises(ValueError):
                run(client.list_events(tenant))
        run(client.list_events("../..%2e/admin"))
        assert len(rec.requests) == 1
        assert rec.requests[0].url.raw_path.decode() == "/api/v2/tenants/..%2F..%252e%2Fadmin/mcp/events"

    def test_subscribe_puts_params_untouched_and_returns_result_verbatim(self) -> None:
        result = {
            "id": "sub_1",
            "refreshBefore": "2026-10-09T18:00:00Z",
            "cursor": None,
            "truncated": False,
            "deliveryStatus": {"active": True, "lastDeliveryAt": None, "lastError": None},
            "futureField": {"nested": [1, 2]},
        }
        rec = Recorder(lambda r: json_response(200, result))

        async def key() -> str:
            return "Bearer already"

        client = rec.client(api_key=key)
        params = {
            "name": "order.created",
            "arguments": {"total": {"$gte": 100}},
            "delivery": {"mode": "webhook", "url": "https://r.example/cb", "secret": "whsec_x"},
            "cursor": None,
            "_meta": {"unknown": True},
        }

        got = run(client.subscribe("t", {"principal": "user_1", "params": params}))

        assert got == result
        req = rec.requests[0]
        assert req.method == "PUT"
        assert req.url.path == "/api/v2/tenants/t/mcp/subscriptions"
        assert req.headers["authorization"] == "Bearer already"
        assert req.headers["content-type"] == "application/json"
        assert json.loads(req.content) == {"principal": "user_1", "params": params}

    def test_unsubscribe_posts(self) -> None:
        rec = Recorder(lambda r: json_response(200, {}))
        assert run(rec.client().unsubscribe("t", {"principal": "p", "params": {}})) == {}
        assert rec.requests[0].method == "POST"
        assert rec.requests[0].url.path == "/api/v2/tenants/t/mcp/subscriptions/unsubscribe"

    def test_mcp_error_raises_mcp_error(self) -> None:
        rec = Recorder(lambda r: json_response(422, {"mcp_error": MCP_ERROR}))
        with pytest.raises(MCPError) as exc:
            run(rec.client().subscribe("t", {"principal": "p", "params": {}}))
        err = exc.value
        assert (err.status_code, err.kind, err.code, err.message, err.data) == (
            422,
            "callback_endpoint_error",
            -32015,
            "CallbackEndpointError",
            {"reason": "challenge_failed"},
        )
        assert err.mcp_error == MCP_ERROR

    @pytest.mark.parametrize(
        "status,body",
        [
            (422, {"message": "validation error", "data": ["x"]}),
            (422, {"mcp_error": {"kind": "x", "code": "not-a-number", "message": "m"}}),
            (422, {"mcp_error": {"code": True, "message": "m"}}),
            (500, "x" * 5000),
            (503, ""),
            (302, ""),
            (200, "[]"),
            (200, "not json"),
            (200, ""),
            (200, "null"),
        ],
    )
    def test_other_failures_raise_request_error(self, status: int, body: Any) -> None:
        rec = Recorder(lambda r: json_response(status, body))
        with pytest.raises(MCPEventsRequestError) as exc:
            run(rec.client().list_events("t"))
        assert exc.value.status_code == status
        assert len(exc.value.body or "") <= 1024

    def test_redirects_are_not_followed(self) -> None:
        def respond(request: httpx.Request) -> httpx.Response:
            if request.url.path == "/target":
                return json_response(200, {"leaked": request.headers.get("authorization")})
            return httpx.Response(302, headers={"location": "https://outpost.example.com/target"})

        rec = Recorder(respond)
        with pytest.raises(MCPEventsRequestError):
            run(rec.client(api_key="secret").list_events("t"))
        assert [r.url.path for r in rec.requests] == ["/api/v2/tenants/t/mcp/events"]

    def test_oversized_response_from_content_length(self) -> None:
        rec = Recorder(lambda r: httpx.Response(200, headers={"content-length": "999999999"}, content=b"{}"))
        with pytest.raises(MCPEventsRequestError, match="too large"):
            run(rec.client(max_response_bytes=1024).list_events("t"))

    def test_oversized_streamed_response(self) -> None:
        async def stream() -> Any:
            for _ in range(64):
                yield b"x" * 65536

        rec = Recorder(lambda r: httpx.Response(200, content=stream()))
        with pytest.raises(MCPEventsRequestError, match="too large"):
            run(rec.client(max_response_bytes=256 * 1024).list_events("t"))

    def test_timeout(self) -> None:
        async def slow(request: httpx.Request) -> httpx.Response:
            await asyncio.sleep(5)
            return json_response(200, {})

        rec = Recorder(slow)
        loop_time: list[float] = []

        async def go() -> None:
            start = asyncio.get_running_loop().time()
            try:
                await rec.client(timeout_ms=100).list_events("t")
            finally:
                loop_time.append(asyncio.get_running_loop().time() - start)

        with pytest.raises(MCPEventsRequestError, match="timed out"):
            run(go())
        assert loop_time[0] < 2

    def test_network_failure(self) -> None:
        def fail(request: httpx.Request) -> httpx.Response:
            raise httpx.ConnectError("connect failed", request=request)

        with pytest.raises(MCPEventsRequestError) as exc:
            run(Recorder(fail).client().list_events("t"))
        assert exc.value.status_code is None

    def test_body_must_be_json(self) -> None:
        rec = Recorder(lambda r: json_response(200, {}))
        with pytest.raises(MCPEventsRequestError):
            run(rec.client().subscribe("t", {"principal": "p", "params": {"x": float("nan")}}))
        assert not rec.requests

    def test_owned_client_is_closed(self) -> None:
        async def go() -> None:
            async with MCPEventsClient("http://localhost:1/api/v2") as client:
                assert client.base_url == "http://localhost:1/api/v2/"

        run(go())


class TestCreateClient:
    def test_from_outpost_instance(self) -> None:
        rec = Recorder(lambda r: json_response(200, {"events": []}))
        outpost = Outpost(
            api_key="admin-key",
            server_url="http://localhost:3333/api/v1",
            async_client=httpx.AsyncClient(transport=httpx.MockTransport(rec.handle)),
        )
        client = create_client(outpost)
        run(client.list_events("t"))
        assert str(rec.requests[0].url) == "http://localhost:3333/api/v2/tenants/t/mcp/events"
        assert rec.requests[0].headers["authorization"] == "Bearer admin-key"

    def test_from_outpost_with_callable_key(self) -> None:
        rec = Recorder(lambda r: json_response(200, {}))
        outpost = Outpost(
            api_key=lambda: "rotating",
            server_url="http://localhost:3333/api/v2",
            async_client=httpx.AsyncClient(transport=httpx.MockTransport(rec.handle)),
        )
        run(create_client(outpost).unsubscribe("t", {"principal": "p", "params": {}}))
        assert rec.requests[0].headers["authorization"] == "Bearer rotating"

    def test_from_mapping_and_passthrough(self) -> None:
        client = create_client({"server_url": "http://h/api/v2", "api_key": "k"})
        assert isinstance(client, MCPEventsClient)
        assert create_client(client) is client
        with pytest.raises(TypeError):
            create_client({})
        with pytest.raises(TypeError):
            create_client(None)


class TestMCPErrorFrom:
    def test_duck_typing(self) -> None:
        class WithAttr(Exception):
            mcp_error = MCP_ERROR

        class Model:
            def __init__(self, **kw: Any) -> None:
                self.__dict__.update(kw)

        class Generated(Exception):
            # Like a generated error class: data.mcp_error is a model.
            data = Model(mcp_error=Model(**MCP_ERROR))

        class HTTPError(Exception):
            status_code = 422
            body = json.dumps({"mcp_error": MCP_ERROR})

        assert mcp_error_from(MCPError(dict(MCP_ERROR))) == MCP_ERROR
        assert mcp_error_from(WithAttr()) == MCP_ERROR
        assert mcp_error_from(Generated()) == MCP_ERROR
        assert mcp_error_from(HTTPError()) == MCP_ERROR

    def test_ignores_everything_else(self) -> None:
        class Bad(Exception):
            def __init__(self, **kw: Any) -> None:
                super().__init__()
                self.__dict__.update(kw)

        assert mcp_error_from(Exception("x")) is None
        assert mcp_error_from(Bad(mcp_error={"code": 1.5, "message": "m"})) is None
        assert mcp_error_from(Bad(mcp_error={"code": -1})) is None
        assert mcp_error_from(Bad(status_code=500, body=json.dumps({"mcp_error": MCP_ERROR}))) is None
        assert mcp_error_from(Bad(status_code=422, body="{")) is None
        assert mcp_error_from(Bad(status_code=422, body=" " * (70 * 1024))) is None


class FakeAPI:
    """A fake MCP Events client recording calls."""

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


def raising(err: BaseException) -> Any:
    def answer(*_: Any) -> Any:
        raise err

    return answer


def tenant_of(principal: str, _context: Any) -> str:
    return f"tenant_of_{principal}"


def rpc_error(coro: Any) -> JSONRPCError:
    with pytest.raises(JSONRPCError) as exc:
        run(coro)
    return exc.value


class TestHandlers:
    def test_requires_resolve_tenant(self) -> None:
        with pytest.raises(TypeError):
            MCPEventsHandlers(FakeAPI(), resolve_tenant=None)  # type: ignore[arg-type]

    def test_forwards_each_method(self) -> None:
        api = FakeAPI()
        h = MCPEventsHandlers(api, resolve_tenant=tenant_of)
        params = {"name": "order.created", "arguments": {}, "delivery": {"url": "https://x"}}

        assert run(h.handle_list("u1", {"cursor": "c1"})) == {"from": "list_events"}
        assert run(h.handle_subscribe("u1", params)) == {"from": "subscribe"}
        assert run(h.handle_unsubscribe("u1", params)) == {"from": "unsubscribe"}
        assert api.calls == [
            ("list_events", "tenant_of_u1", {"cursor": "c1", "topics": None}),
            ("subscribe", "tenant_of_u1", {"principal": "u1", "params": params}),
            ("unsubscribe", "tenant_of_u1", {"principal": "u1", "params": params}),
        ]

    @pytest.mark.parametrize("principal", [None, "", 42])
    def test_missing_principal_is_forbidden(self, principal: Any) -> None:
        api = FakeAPI()
        resolved: list[Any] = []
        h = MCPEventsHandlers(api, resolve_tenant=lambda p, c: resolved.append(p) or "t")
        for call in (h.handle_list, h.handle_subscribe, h.handle_unsubscribe):
            err = rpc_error(call(principal, {}))
            assert (err.code, err.message, err.data) == (-32012, "Forbidden", None)
        assert not api.calls and not resolved

    def test_sep_3415_profile(self) -> None:
        h = MCPEventsHandlers(FakeAPI(), resolve_tenant=tenant_of, allowed_topics=lambda p, c: ["a"], codes="sep-3415")
        assert rpc_error(h.handle_subscribe(None, {})).code == -32024
        err = rpc_error(h.handle_subscribe("u", {"name": "b"}))
        assert (err.code, err.message, err.data) == (-32023, "NotFound", {"kind": "event"})

    def test_custom_and_unknown_codes(self) -> None:
        h = MCPEventsHandlers(FakeAPI(), resolve_tenant=tenant_of, codes=ErrorCodes(1, 2, 3, 4, 5, 6, 7))
        assert rpc_error(h.handle_list("", {})).code == 3
        with pytest.raises(ValueError):
            MCPEventsHandlers(FakeAPI(), resolve_tenant=tenant_of, codes="nope")

    @pytest.mark.parametrize("tenant", [None, "", 7])
    def test_principal_without_tenant_is_forbidden(self, tenant: Any) -> None:
        api = FakeAPI()
        seen: list[Any] = []

        async def resolve(principal: str, context: Any) -> Any:
            seen.append((principal, context))
            return tenant

        h = MCPEventsHandlers(api, resolve_tenant=resolve)
        assert rpc_error(h.handle_list("u", {}, {"req": 1})).code == -32012
        assert not api.calls
        assert seen == [("u", {"req": 1})]

    @pytest.mark.parametrize("params", [[], "x", 1, True])
    def test_params_must_be_a_mapping(self, params: Any) -> None:
        h = MCPEventsHandlers(FakeAPI(), resolve_tenant=tenant_of)
        err = rpc_error(h.handle_subscribe("u", params))
        assert (err.code, err.message, err.data) == (-32602, "InvalidParams", {"field": "params", "reason": "invalid"})

    def test_absent_params_are_empty(self) -> None:
        api = FakeAPI()
        h = MCPEventsHandlers(api, resolve_tenant=tenant_of)
        run(h.handle_unsubscribe("u", None))
        assert api.calls == [("unsubscribe", "tenant_of_u", {"principal": "u", "params": {}})]

    @pytest.mark.parametrize("cursor", [1, {}, []])
    def test_non_string_cursor_is_invalid(self, cursor: Any) -> None:
        api = FakeAPI()
        h = MCPEventsHandlers(api, resolve_tenant=tenant_of)
        assert rpc_error(h.handle_list("u", {"cursor": cursor})).data == {"field": "cursor", "reason": "invalid"}
        assert not api.calls

    def test_allowed_topics_narrows_list_and_empty_answers_locally(self) -> None:
        api = FakeAPI(lambda *_: {"events": ["from outpost"]})
        allowed: list[str] = ["order.created", "order.paid"]

        async def topics(principal: str, context: Any) -> list[str]:
            return allowed

        h = MCPEventsHandlers(api, resolve_tenant=tenant_of, allowed_topics=topics)
        assert run(h.handle_list("u", {})) == {"events": ["from outpost"]}
        assert api.calls[0][2] == {"cursor": None, "topics": ["order.created", "order.paid"]}

        allowed = []
        assert run(h.handle_list("u", {})) == {"events": []}
        allowed = ["", "a,b"]
        assert run(h.handle_list("u", {"cursor": "c"})) == {"events": []}
        assert len(api.calls) == 1

    def test_subscribe_outside_allowlist_is_not_found(self) -> None:
        api = FakeAPI()
        h = MCPEventsHandlers(api, resolve_tenant=tenant_of, allowed_topics=lambda p, c: ("order.created",))
        err = rpc_error(h.handle_subscribe("u", {"name": "order.refunded"}))
        assert (err.code, err.data) == (-32011, {"kind": "event"})
        assert not api.calls
        run(h.handle_subscribe("u", {"name": "order.created"}))
        assert api.calls[0][2] == {
            "principal": "u",
            "params": {"name": "order.created"},
            "allowed_topics": ["order.created"],
        }

    def test_empty_allowlist_rejects_every_subscribe(self) -> None:
        api = FakeAPI()
        h = MCPEventsHandlers(api, resolve_tenant=tenant_of, allowed_topics=lambda p, c: [])
        assert rpc_error(h.handle_subscribe("u", {"name": "order.created"})).code == -32011
        assert not api.calls

    def test_non_string_name_is_left_to_outpost_with_the_allowlist(self) -> None:
        api = FakeAPI()
        h = MCPEventsHandlers(api, resolve_tenant=tenant_of, allowed_topics=lambda p, c: ["a"])
        run(h.handle_subscribe("u", {"name": ["a"]}))
        assert api.calls[0][2] == {"principal": "u", "params": {"name": ["a"]}, "allowed_topics": ["a"]}

    @pytest.mark.parametrize(
        "params",
        [
            {"name": "allowed", "Name": "secret.topic"},
            {"NAME": "secret.topic"},
            {"nAmE": "secret.topic", "name": "allowed"},
        ],
    )
    def test_differently_cased_name_cannot_smuggle_a_topic(self, params: dict[str, Any]) -> None:
        # Go's JSON decoding (Outpost) matches member names case-insensitively.
        api = FakeAPI()
        h = MCPEventsHandlers(api, resolve_tenant=tenant_of, allowed_topics=lambda p, c: ["allowed"])
        assert rpc_error(h.handle_subscribe("u", params)).code == -32011
        assert not api.calls

    def test_unsubscribe_is_never_filtered(self) -> None:
        api = FakeAPI(lambda *_: {})
        asked: list[Any] = []
        h = MCPEventsHandlers(api, resolve_tenant=tenant_of, allowed_topics=lambda p, c: asked.append(p) or [])
        assert run(h.handle_unsubscribe("u", {"name": "anything"})) == {}
        assert len(api.calls) == 1 and not asked

    @pytest.mark.parametrize("bad", [None, "order.created", 3])
    def test_allowed_topics_must_return_a_sequence(self, bad: Any) -> None:
        errors: list[Any] = []
        h = MCPEventsHandlers(
            FakeAPI(), resolve_tenant=tenant_of, allowed_topics=lambda p, c: bad, on_error=lambda e, m: errors.append(e)
        )
        assert rpc_error(h.handle_list("u", {})).code == -32603
        assert isinstance(errors[0], TypeError)

    def test_mcp_error_is_rethrown_verbatim(self) -> None:
        class HTTPError(Exception):
            status_code = 422
            body = json.dumps({"mcp_error": MCP_ERROR})

        class WithAttr(Exception):
            mcp_error = MCP_ERROR

        for err in (MCPError(dict(MCP_ERROR)), WithAttr(), HTTPError()):
            h = MCPEventsHandlers(FakeAPI(raising(err)), resolve_tenant=tenant_of, codes="sep-3415")
            got = rpc_error(h.handle_subscribe("u", {}))
            # Outpost's own code wins over the local profile.
            assert (got.code, got.message, got.data) == (-32015, "CallbackEndpointError", {"reason": "challenge_failed"})
            assert got.__cause__ is None and got.__suppress_context__

    def test_other_failures_are_internal_and_leak_nothing(self) -> None:
        secret_body = '{"message":"boom","data":["whsec_supersecret"]}'
        failures: list[BaseException] = [
            MCPEventsRequestError("Outpost responded with status 500", status_code=500, body=secret_body),
            ConnectionError("connect ECONNREFUSED 10.0.0.1:3333"),
            MCPEventsRequestError("bad", status_code=400, body=secret_body),
        ]
        for failure in failures:
            reported: list[Any] = []

            def on_error(err: BaseException, method: str) -> None:
                reported.append((err, method))
                raise RuntimeError("a broken logger")

            h = MCPEventsHandlers(FakeAPI(raising(failure)), resolve_tenant=tenant_of, on_error=on_error)
            err = rpc_error(h.handle_subscribe("u", {}))
            assert (err.code, err.message, err.data) == (-32603, "Internal error", None)
            assert "whsec_" not in repr(err.__dict__) and "10.0.0.1" not in str(err)
            assert err.__cause__ is None and err.__suppress_context__
            assert reported == [(failure, "events/subscribe")]

    @pytest.mark.parametrize("result", [None, [], "x", 1])
    def test_non_object_result_is_internal(self, result: Any) -> None:
        h = MCPEventsHandlers(FakeAPI(lambda *_: result), resolve_tenant=tenant_of)
        assert rpc_error(h.handle_list("u", {})).code == -32603

    def test_resolver_errors(self) -> None:
        def resolve(principal: str, context: Any) -> str:
            if principal == "custom":
                raise JSONRPCError(-32012, "Forbidden", {"reason": "suspended"})
            raise RuntimeError("db down")

        h = MCPEventsHandlers(FakeAPI(), resolve_tenant=resolve)
        assert rpc_error(h.handle_list("u", {})).code == -32603
        err = rpc_error(h.handle_list("custom", {}))
        assert (err.code, err.data) == (-32012, {"reason": "suspended"})

    def test_cancellation_is_not_swallowed(self) -> None:
        h = MCPEventsHandlers(FakeAPI(raising(asyncio.CancelledError())), resolve_tenant=tenant_of)
        with pytest.raises(asyncio.CancelledError):
            run(h.handle_list("u", {}))

    def test_end_to_end_with_the_http_client(self) -> None:
        rec = Recorder(
            lambda r: json_response(422, {"mcp_error": MCP_ERROR})
            if r.method == "PUT"
            else json_response(200, {"events": [{"name": "a"}]})
        )
        h = MCPEventsHandlers(rec.client(api_key="k"), resolve_tenant=tenant_of, allowed_topics=lambda p, c: ["a"])
        assert run(h.handle_list("u", {})) == {"events": [{"name": "a"}]}
        assert rpc_error(h.handle_subscribe("u", {"name": "a"})).code == -32015
        assert [(r.method, r.url.raw_path.decode()) for r in rec.requests] == [
            ("GET", "/api/v2/tenants/tenant_of_u/mcp/events?topics=a"),
            ("PUT", "/api/v2/tenants/tenant_of_u/mcp/subscriptions"),
        ]


def test_register_rejects_unsupported_servers() -> None:
    with pytest.raises(TypeError, match="version 2 or later"):
        mcp_events.register(
            FakeAPI(), object(), resolve_principal=lambda ctx: "u", resolve_tenant=tenant_of
        )
    with pytest.raises(TypeError):
        mcp_events.register(FakeAPI(), object(), resolve_principal=None, resolve_tenant=tenant_of)  # type: ignore[arg-type]


def test_capability_middleware_adds_events_to_initialize_and_discover() -> None:
    # pylint: disable=protected-access
    class Ctx:
        def __init__(self, method: str) -> None:
            self.method = method

    async def call_next_for(result: Any) -> Any:
        async def call_next(_ctx: Any) -> Any:
            return result

        return call_next

    async def go() -> None:
        for method in ("initialize", "server/discover"):
            result = {"capabilities": {"tools": {}}, "other": 1}
            got = await mcp_events._advertise_events(Ctx(method), await call_next_for(result))
            assert got == {"capabilities": {"tools": {}, "events": {}}, "other": 1}
            assert result == {"capabilities": {"tools": {}}, "other": 1}  # not mutated
        kept = {"capabilities": {"events": {"listChanged": True}}}
        assert await mcp_events._advertise_events(Ctx("initialize"), await call_next_for(kept)) is kept
        listed = {"events": []}
        assert await mcp_events._advertise_events(Ctx("events/list"), await call_next_for(listed)) is listed
        assert await mcp_events._advertise_events(Ctx("initialize"), await call_next_for(None)) is None

    run(go())
