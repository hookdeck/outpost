"""Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT."""

from __future__ import annotations
from .hookdeckcredentials import HookdeckCredentials, HookdeckCredentialsTypedDict
from .topics_union import TopicsUnion, TopicsUnionTypedDict
from outpost_sdk.types import BaseModel
from typing import Any, Optional
from typing_extensions import NotRequired, TypedDict


class DestinationUpdateHookdeckTypedDict(TypedDict):
    topics: NotRequired[TopicsUnionTypedDict]
    r"""\"*\" or an array of enabled topics."""
    config: NotRequired[Any]
    credentials: NotRequired[HookdeckCredentialsTypedDict]


class DestinationUpdateHookdeck(BaseModel):
    topics: Optional[TopicsUnion] = None
    r"""\"*\" or an array of enabled topics."""

    config: Optional[Any] = None

    credentials: Optional[HookdeckCredentials] = None
