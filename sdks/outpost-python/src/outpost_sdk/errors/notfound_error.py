"""Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT."""

from __future__ import annotations
import httpx
from outpost_sdk.errors import OutpostError
from outpost_sdk.types import BaseModel
import pydantic
from typing import Any, Dict, Optional
from typing_extensions import Annotated


class NotFoundErrorData(BaseModel):
    message: Optional[str] = None

    additional_properties: Annotated[
        Optional[Dict[str, Any]], pydantic.Field(exclude=True)
    ] = None


class NotFoundError(OutpostError):
    r"""Status codes relating to the resource/entity they are requesting not being found or endpoints/routes not existing"""

    data: NotFoundErrorData

    def __init__(
        self,
        data: NotFoundErrorData,
        raw_response: httpx.Response,
        body: Optional[str] = None,
    ):
        fallback = body or raw_response.text
        message = str(data.message) or fallback
        super().__init__(message, raw_response, body)
        self.data = data
