"""Tests for filter/metadata/delivery_metadata removal via Python SDK."""

import os
import pytest
from outpost_sdk import Outpost
from outpost_sdk.models import (
    DestinationCreateWebhook,
    DestinationUpdateWebhook,
    WebhookConfig,
)
from outpost_sdk.types.basemodel import Unset

API_KEY = os.getenv("API_KEY", "apikey")
API_BASE_URL = os.getenv("API_BASE_URL", "http://localhost:3333/api/v1")
TENANT_ID = "python-sdk-test-tenant"


def is_empty_or_unset(val):
    """Check if a field is empty, None, or Unset."""
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


def create_webhook(client, *, filter_=None, metadata=None, delivery_metadata=None):
    kwargs = {
        "type": "webhook",
        "topics": ["user.created", "user.updated"],
        "config": WebhookConfig(url="https://example.com/webhook"),
    }
    if filter_ is not None:
        kwargs["filter_"] = filter_
    if metadata is not None:
        kwargs["metadata"] = metadata
    if delivery_metadata is not None:
        kwargs["delivery_metadata"] = delivery_metadata

    return client.destinations.create(
        tenant_id=TENANT_ID,
        body=DestinationCreateWebhook(**kwargs),
    )


def get_webhook(client, dest_id):
    return client.destinations.get(tenant_id=TENANT_ID, destination_id=dest_id)


def update_webhook(client, dest_id, **kwargs):
    return client.destinations.update(
        tenant_id=TENANT_ID,
        destination_id=dest_id,
        body=DestinationUpdateWebhook(**kwargs),
    )


def delete_webhook(client, dest_id):
    try:
        client.destinations.delete(tenant_id=TENANT_ID, destination_id=dest_id)
    except Exception:
        pass


class TestFilterRemoval:
    @pytest.fixture(autouse=True)
    def setup(self, client):
        dest = create_webhook(client, filter_={"body": {"user_id": "usr_123"}})
        self.dest_id = dest.id
        self.client = client
        yield
        delete_webhook(client, self.dest_id)

    def test_filter_set_after_creation(self):
        dest = get_webhook(self.client, self.dest_id)
        assert not isinstance(dest.filter_, Unset)
        assert dest.filter_ is not None
        assert dest.filter_["body"]["user_id"] == "usr_123"

    def test_clear_filter_with_empty_dict(self):
        update_webhook(self.client, self.dest_id, filter_={})
        dest = get_webhook(self.client, self.dest_id)
        assert is_empty_or_unset(dest.filter_)

    def test_no_change_when_field_omitted(self):
        update_webhook(self.client, self.dest_id, filter_={"body": {"user_id": "usr_456"}})
        update_webhook(self.client, self.dest_id, topics=["user.created", "user.updated"])
        dest = get_webhook(self.client, self.dest_id)
        assert dest.filter_["body"]["user_id"] == "usr_456"


class TestMetadataRemoval:
    @pytest.fixture(autouse=True)
    def setup(self, client):
        dest = create_webhook(
            client,
            metadata={"env": "production", "team": "platform", "region": "us-east-1"},
        )
        self.dest_id = dest.id
        self.client = client
        yield
        delete_webhook(client, self.dest_id)

    def test_metadata_set_after_creation(self):
        dest = get_webhook(self.client, self.dest_id)
        assert dest.metadata == {
            "env": "production",
            "team": "platform",
            "region": "us-east-1",
        }

    def test_remove_single_metadata_key(self):
        update_webhook(
            self.client,
            self.dest_id,
            metadata={"env": "production", "team": "platform"},
        )
        dest = get_webhook(self.client, self.dest_id)
        assert dest.metadata == {"env": "production", "team": "platform"}
        assert "region" not in dest.metadata

    def test_clear_metadata_with_empty_dict(self):
        update_webhook(self.client, self.dest_id, metadata={})
        dest = get_webhook(self.client, self.dest_id)
        assert is_empty_or_unset(dest.metadata)

    def test_no_change_when_field_omitted(self):
        update_webhook(self.client, self.dest_id, metadata={"env": "staging"})
        update_webhook(self.client, self.dest_id, topics=["user.created", "user.updated"])
        dest = get_webhook(self.client, self.dest_id)
        assert dest.metadata == {"env": "staging"}


class TestDeliveryMetadataRemoval:
    @pytest.fixture(autouse=True)
    def setup(self, client):
        dest = create_webhook(
            client,
            delivery_metadata={"source": "outpost", "version": "1.0"},
        )
        self.dest_id = dest.id
        self.client = client
        yield
        delete_webhook(client, self.dest_id)

    def test_delivery_metadata_set_after_creation(self):
        dest = get_webhook(self.client, self.dest_id)
        assert dest.delivery_metadata == {"source": "outpost", "version": "1.0"}

    def test_remove_single_delivery_metadata_key(self):
        update_webhook(
            self.client,
            self.dest_id,
            delivery_metadata={"source": "outpost"},
        )
        dest = get_webhook(self.client, self.dest_id)
        assert dest.delivery_metadata == {"source": "outpost"}
        assert "version" not in dest.delivery_metadata

    def test_clear_delivery_metadata_with_empty_dict(self):
        update_webhook(self.client, self.dest_id, delivery_metadata={})
        dest = get_webhook(self.client, self.dest_id)
        assert is_empty_or_unset(dest.delivery_metadata)

    def test_no_change_when_field_omitted(self):
        update_webhook(self.client, self.dest_id, delivery_metadata={"source": "test"})
        update_webhook(self.client, self.dest_id, topics=["user.created", "user.updated"])
        dest = get_webhook(self.client, self.dest_id)
        assert dest.delivery_metadata == {"source": "test"}
