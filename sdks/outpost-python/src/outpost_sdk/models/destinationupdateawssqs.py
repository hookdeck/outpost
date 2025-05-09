"""Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT."""

from __future__ import annotations
from .awssqsconfig import AWSSQSConfig, AWSSQSConfigTypedDict
from .awssqscredentials import AWSSQSCredentials, AWSSQSCredentialsTypedDict
from .topics_union import TopicsUnion, TopicsUnionTypedDict
from outpost_sdk.types import BaseModel
from typing import Optional
from typing_extensions import NotRequired, TypedDict


class DestinationUpdateAWSSQSTypedDict(TypedDict):
    topics: NotRequired[TopicsUnionTypedDict]
    r"""\"*\" or an array of enabled topics."""
    config: NotRequired[AWSSQSConfigTypedDict]
    credentials: NotRequired[AWSSQSCredentialsTypedDict]


class DestinationUpdateAWSSQS(BaseModel):
    topics: Optional[TopicsUnion] = None
    r"""\"*\" or an array of enabled topics."""

    config: Optional[AWSSQSConfig] = None

    credentials: Optional[AWSSQSCredentials] = None
