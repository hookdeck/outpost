"""Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT."""

from __future__ import annotations
from outpost_sdk.types import BaseModel
from typing import Optional
from typing_extensions import NotRequired, TypedDict


class AWSKinesisConfigTypedDict(TypedDict):
    stream_name: str
    r"""The name of the AWS Kinesis stream."""
    region: str
    r"""The AWS region where the Kinesis stream is located."""
    endpoint: NotRequired[str]
    r"""Optional. Custom AWS endpoint URL (e.g., for LocalStack or VPC endpoints)."""
    partition_key_template: NotRequired[str]
    r"""Optional. JMESPath template to extract the partition key from the event payload (e.g., `metadata.\"event-id\"`). Defaults to event ID."""


class AWSKinesisConfig(BaseModel):
    stream_name: str
    r"""The name of the AWS Kinesis stream."""

    region: str
    r"""The AWS region where the Kinesis stream is located."""

    endpoint: Optional[str] = None
    r"""Optional. Custom AWS endpoint URL (e.g., for LocalStack or VPC endpoints)."""

    partition_key_template: Optional[str] = None
    r"""Optional. JMESPath template to extract the partition key from the event payload (e.g., `metadata.\"event-id\"`). Defaults to event ID."""
