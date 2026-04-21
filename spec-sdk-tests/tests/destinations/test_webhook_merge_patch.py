"""Tests for webhook destination merge-patch semantics (RFC 7396) via Python SDK."""

import os
import pytest
from outpost_sdk import Outpost
from outpost_sdk.models import (
    DestinationCreateWebhook,
    DestinationUpdateWebhook,
    WebhookConfig,
)
from outpost_sdk.types.basemodel import Unset

API_KEY = os.getenv("API_KEY", "test-api-key")
API_BASE_URL = os.getenv("API_BASE_URL", "http://localhost:3333/api/v1")
TENANT_ID = "python-merge-patch-test"


def is_empty_or_unset(val):
    if val is None or isinstance(val, Unset):
        return True
    if isinstance(val, dict) and len(val) == 0:
        return True
    return False


@pytest.fixture(scope="module")
def client():
    return Outpost(api_key=API_KEY, server_url=API_BASE_URL)


@pytest.fixture(scope="module", autouse=True)
def setup_tenant(client):
    client.tenants.upsert(tenant_id=TENANT_ID)
    yield
    try:
        client.tenants.delete(tenant_id=TENANT_ID)
    except Exception:
        pass


def create_dest(client, *, filter_=None, metadata=None, delivery_metadata=None):
    kwargs = {
        "type": "webhook",
        "topics": ["*"],
        "config": WebhookConfig(url="https://example.com/webhook"),
    }
    if filter_ is not None:
        kwargs["filter_"] = filter_
    if metadata is not None:
        kwargs["metadata"] = metadata
    if delivery_metadata is not None:
        kwargs["delivery_metadata"] = delivery_metadata

    return client.destinations.create(tenant_id=TENANT_ID, body=DestinationCreateWebhook(**kwargs))


def update_dest(client, dest_id, **kwargs):
    return client.destinations.update(
        tenant_id=TENANT_ID,
        destination_id=dest_id,
        body=DestinationUpdateWebhook(**kwargs),
    )


def get_dest(client, dest_id):
    return client.destinations.get(tenant_id=TENANT_ID, destination_id=dest_id)


def delete_dest(client, dest_id):
    try:
        client.destinations.delete(tenant_id=TENANT_ID, destination_id=dest_id)
    except Exception:
        pass


# ── metadata merge-patch ──


class TestMetadataMergePatch:
    @pytest.fixture(autouse=True)
    def setup(self, client):
        self.client = client
        dest = create_dest(client, metadata={"env": "prod", "region": "us-east-1"})
        self.dest_id = dest.id
        yield
        delete_dest(client, self.dest_id)

    def test_add_key_preserving_existing(self):
        updated = update_dest(self.client, self.dest_id, metadata={"env": "prod", "region": "us-east-1", "team": "platform"})
        dest = get_dest(self.client, self.dest_id)
        assert dest.metadata == {"env": "prod", "region": "us-east-1", "team": "platform"}

    def test_update_existing_key(self):
        updated = update_dest(self.client, self.dest_id, metadata={"env": "staging"})
        dest = get_dest(self.client, self.dest_id)
        assert dest.metadata["env"] == "staging"
        assert dest.metadata["region"] == "us-east-1"

    def test_delete_key_via_null(self):
        updated = update_dest(self.client, self.dest_id, metadata={"env": "prod", "region": None})
        dest = get_dest(self.client, self.dest_id)
        assert dest.metadata == {"env": "prod"}
        assert "region" not in dest.metadata

    def test_clear_entire_field_via_null(self):
        update_dest(self.client, self.dest_id, metadata=None)
        dest = get_dest(self.client, self.dest_id)
        assert is_empty_or_unset(dest.metadata)

    def test_empty_object_is_noop(self):
        update_dest(self.client, self.dest_id, metadata={})
        dest = get_dest(self.client, self.dest_id)
        assert dest.metadata == {"env": "prod", "region": "us-east-1"}

    def test_omitted_is_noop(self):
        update_dest(self.client, self.dest_id, topics=["*"])
        dest = get_dest(self.client, self.dest_id)
        assert dest.metadata == {"env": "prod", "region": "us-east-1"}

    def test_mixed_add_update_delete(self):
        dest = create_dest(self.client, metadata={"keep": "v", "remove": "v", "update": "old"})
        try:
            update_dest(self.client, dest.id, metadata={"keep": "v", "remove": None, "update": "new", "add": "v"})
            got = get_dest(self.client, dest.id)
            assert got.metadata == {"keep": "v", "update": "new", "add": "v"}
            assert "remove" not in got.metadata
        finally:
            delete_dest(self.client, dest.id)


# ── delivery_metadata merge-patch ──


class TestDeliveryMetadataMergePatch:
    @pytest.fixture(autouse=True)
    def setup(self, client):
        self.client = client
        dest = create_dest(client, delivery_metadata={"source": "outpost", "version": "1.0"})
        self.dest_id = dest.id
        yield
        delete_dest(client, self.dest_id)

    def test_delete_key_via_null(self):
        update_dest(self.client, self.dest_id, delivery_metadata={"source": "outpost", "version": None})
        dest = get_dest(self.client, self.dest_id)
        assert dest.delivery_metadata == {"source": "outpost"}

    def test_clear_via_null(self):
        update_dest(self.client, self.dest_id, delivery_metadata=None)
        dest = get_dest(self.client, self.dest_id)
        assert is_empty_or_unset(dest.delivery_metadata)

    def test_empty_object_is_noop(self):
        update_dest(self.client, self.dest_id, delivery_metadata={})
        dest = get_dest(self.client, self.dest_id)
        assert dest.delivery_metadata == {"source": "outpost", "version": "1.0"}


# ── filter replacement ──


class TestFilterReplacement:
    @pytest.fixture(autouse=True)
    def setup(self, client):
        self.client = client
        dest = create_dest(client, filter_={"body": {"user_id": "usr_123"}})
        self.dest_id = dest.id
        yield
        delete_dest(client, self.dest_id)

    def test_replace_entirely(self):
        update_dest(self.client, self.dest_id, filter_={"body": {"status": "active"}})
        dest = get_dest(self.client, self.dest_id)
        assert dest.filter_["body"]["status"] == "active"
        assert "user_id" not in dest.filter_.get("body", {})

    def test_clear_with_empty_object(self):
        update_dest(self.client, self.dest_id, filter_={})
        dest = get_dest(self.client, self.dest_id)
        assert is_empty_or_unset(dest.filter_)

    def test_clear_with_null(self):
        update_dest(self.client, self.dest_id, filter_=None)
        dest = get_dest(self.client, self.dest_id)
        assert is_empty_or_unset(dest.filter_)

    def test_unchanged_when_omitted(self):
        update_dest(self.client, self.dest_id, topics=["*"])
        dest = get_dest(self.client, self.dest_id)
        assert not isinstance(dest.filter_, Unset)
        assert dest.filter_["body"]["user_id"] == "usr_123"
