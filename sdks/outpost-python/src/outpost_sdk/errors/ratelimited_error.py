"""Code generated by Speakeasy (https://speakeasy.com). DO NOT EDIT."""

from __future__ import annotations
from outpost_sdk import utils
from outpost_sdk.types import BaseModel
import pydantic
from typing import Any, Dict, Optional
from typing_extensions import Annotated


class RateLimitedErrorData(BaseModel):
    message: Optional[str] = None

    additional_properties: Annotated[
        Optional[Dict[str, Any]], pydantic.Field(exclude=True)
    ] = None


class RateLimitedError(Exception):
    r"""Status codes relating to the client being rate limited by the server"""

    data: RateLimitedErrorData

    def __init__(self, data: RateLimitedErrorData):
        self.data = data

    def __str__(self) -> str:
        return utils.marshal_json(self.data, RateLimitedErrorData)
