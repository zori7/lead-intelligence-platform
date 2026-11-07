"""Service configuration from the environment (SPEC.md §13.1).

Only infrastructure lives here. Which model each workflow step uses is runtime
data sent by Go with every request (§9.6).
"""

from __future__ import annotations

from functools import lru_cache
from typing import Literal

from pydantic import Field, HttpUrl
from pydantic_settings import BaseSettings, SettingsConfigDict

DEFAULT_ICP = (
    "B2B SaaS or technology-enabled service companies with 50-1000 employees that sell "
    "to other businesses, have a dedicated sales or revenue team, and are investing in "
    "growth (hiring, new markets, or new product lines)."
)


class Settings(BaseSettings):
    model_config = SettingsConfigDict(env_file=None, extra="ignore")

    grpc_addr: str = "0.0.0.0:50051"
    log_level: Literal["debug", "info", "warning", "error"] = "info"

    llm_base_url: HttpUrl
    llm_api_key: str = Field(default="local", min_length=1)
    llm_structured_output_method: Literal["json_schema", "function_calling", "json_mode"] = (
        "json_schema"
    )
    llm_timeout_seconds: float = Field(default=120, gt=0, le=600)
    llm_max_attempts: int = Field(default=3, ge=1, le=10)

    max_concurrent_analyses: int = Field(default=4, ge=1, le=64)
    icp_description: str = Field(default=DEFAULT_ICP, min_length=1, max_length=2000)

    web_fetch_enabled: bool = True
    web_fetch_timeout_seconds: float = Field(default=10, gt=0, le=60)
    web_fetch_max_chars: int = Field(default=6000, ge=500, le=50_000)

    @property
    def llm_base(self) -> str:
        """Base URL without a trailing slash, e.g. ``http://host:11434/v1``."""
        return str(self.llm_base_url).rstrip("/")


@lru_cache(maxsize=1)
def get_settings() -> Settings:
    return Settings()  # type: ignore[call-arg]  # values come from the environment
