"""Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT."""

from __future__ import annotations
from outpost_sdk.types import BaseModel
from outpost_sdk.utils import FieldMetadata, PathParamMetadata
from typing import Optional
from typing_extensions import Annotated, NotRequired, TypedDict


class GetTenantGlobalsTypedDict(TypedDict):
    tenant_id: NotRequired[str]


class GetTenantGlobals(BaseModel):
    tenant_id: Annotated[
        Optional[str],
        FieldMetadata(path=PathParamMetadata(style="simple", explode=False)),
    ] = None


class GetTenantRequestTypedDict(TypedDict):
    tenant_id: NotRequired[str]
    r"""The ID of the tenant. Required when using AdminApiKey authentication."""


class GetTenantRequest(BaseModel):
    tenant_id: Annotated[
        Optional[str],
        FieldMetadata(path=PathParamMetadata(style="simple", explode=False)),
    ] = None
    r"""The ID of the tenant. Required when using AdminApiKey authentication."""
