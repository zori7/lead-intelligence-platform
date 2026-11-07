"""Error taxonomy of the workflow, mapped to proto ``ErrorCode`` (SPEC.md §9.3, §11.1)."""

from __future__ import annotations

from dataclasses import dataclass
from enum import StrEnum

import openai


class ErrorCode(StrEnum):
    INVALID_INPUT = "invalid_input"
    INSUFFICIENT_DATA = "insufficient_data"
    PROVIDER_ERROR = "provider_error"
    PROVIDER_CONFIG = "provider_config"
    INTERNAL = "internal"


@dataclass(frozen=True)
class Failure:
    """A terminal, user-safe failure. ``message`` is shown in the UI."""

    code: ErrorCode
    message: str
    retryable: bool


def invalid_input(message: str) -> Failure:
    return Failure(ErrorCode.INVALID_INPUT, message, retryable=False)


def insufficient_data() -> Failure:
    return Failure(
        ErrorCode.INSUFFICIENT_DATA,
        "Not enough information to analyze. Add a website, industry or notes and try again.",
        retryable=False,
    )


def internal() -> Failure:
    return Failure(ErrorCode.INTERNAL, "Analysis failed unexpectedly.", retryable=True)


class StructuredOutputError(Exception):
    """The model's reply could not be parsed into the expected schema."""


def classify(exc: BaseException) -> Failure:
    """Map an exception from an LLM call to a failure.

    Configuration problems (bad key, unknown model) are not retryable: retrying
    cannot fix them, an admin must. Everything else is treated as transient.
    """
    if isinstance(exc, openai.AuthenticationError | openai.PermissionDeniedError):
        return Failure(
            ErrorCode.PROVIDER_CONFIG,
            "The AI provider rejected the credentials. An admin must check the configuration.",
            retryable=False,
        )
    if isinstance(exc, openai.NotFoundError):
        return Failure(
            ErrorCode.PROVIDER_CONFIG,
            "The configured AI model is not available. An admin must update the model settings.",
            retryable=False,
        )
    return Failure(
        ErrorCode.PROVIDER_ERROR, "The AI provider failed. Please retry.", retryable=True
    )
