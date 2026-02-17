"""Core typed models for the Sigil Python SDK."""

from __future__ import annotations

from dataclasses import dataclass, field
from datetime import datetime, timezone
from enum import Enum
from typing import Any, Optional


class GenerationMode(str, Enum):
    """Generation execution mode."""

    SYNC = "SYNC"
    STREAM = "STREAM"


class MessageRole(str, Enum):
    """Allowed message roles."""

    USER = "user"
    ASSISTANT = "assistant"
    TOOL = "tool"


class PartKind(str, Enum):
    """Allowed message part kinds."""

    TEXT = "text"
    THINKING = "thinking"
    TOOL_CALL = "tool_call"
    TOOL_RESULT = "tool_result"


class ArtifactKind(str, Enum):
    """Allowed raw artifact kinds."""

    REQUEST = "request"
    RESPONSE = "response"
    TOOLS = "tools"
    PROVIDER_EVENT = "provider_event"


class ConversationRatingValue(str, Enum):
    """Allowed conversation rating values."""

    GOOD = "CONVERSATION_RATING_VALUE_GOOD"
    BAD = "CONVERSATION_RATING_VALUE_BAD"


@dataclass(slots=True)
class ModelRef:
    """Provider/model identity."""

    provider: str = ""
    name: str = ""


@dataclass(slots=True)
class ToolDefinition:
    """Tool definition visible to the model."""

    name: str = ""
    description: str = ""
    type: str = ""
    input_schema_json: bytes = b""


@dataclass(slots=True)
class TokenUsage:
    """Token usage counters for request/response."""

    input_tokens: int = 0
    output_tokens: int = 0
    total_tokens: int = 0
    cache_read_input_tokens: int = 0
    cache_write_input_tokens: int = 0
    reasoning_tokens: int = 0
    cache_creation_input_tokens: int = 0

    def normalize(self) -> "TokenUsage":
        """Returns a copy with `total_tokens` auto-filled when missing."""

        normalized = TokenUsage(
            input_tokens=self.input_tokens,
            output_tokens=self.output_tokens,
            total_tokens=self.total_tokens,
            cache_read_input_tokens=self.cache_read_input_tokens,
            cache_write_input_tokens=self.cache_write_input_tokens,
            reasoning_tokens=self.reasoning_tokens,
            cache_creation_input_tokens=self.cache_creation_input_tokens,
        )
        if normalized.total_tokens == 0:
            normalized.total_tokens = normalized.input_tokens + normalized.output_tokens
        return normalized


@dataclass(slots=True)
class PartMetadata:
    """Provider-specific payload metadata."""

    provider_type: str = ""


@dataclass(slots=True)
class ToolCall:
    """Tool call payload for assistant messages."""

    name: str
    id: str = ""
    input_json: bytes = b""


@dataclass(slots=True)
class ToolResult:
    """Tool result payload for tool-role messages."""

    tool_call_id: str = ""
    name: str = ""
    content: str = ""
    content_json: bytes = b""
    is_error: bool = False


@dataclass(slots=True)
class Part:
    """Typed message part."""

    kind: PartKind
    text: str = ""
    thinking: str = ""
    tool_call: Optional[ToolCall] = None
    tool_result: Optional[ToolResult] = None
    metadata: PartMetadata = field(default_factory=PartMetadata)


@dataclass(slots=True)
class Message:
    """Normalized message payload."""

    role: MessageRole
    parts: list[Part]
    name: str = ""


@dataclass(slots=True)
class Artifact:
    """Optional raw provider artifact."""

    kind: ArtifactKind
    name: str = ""
    content_type: str = ""
    payload: bytes = b""
    record_id: str = ""
    uri: str = ""


@dataclass(slots=True)
class GenerationStart:
    """Seed fields used when generation recording starts."""

    model: ModelRef
    id: str = ""
    conversation_id: str = ""
    agent_name: str = ""
    agent_version: str = ""
    mode: Optional[GenerationMode] = None
    operation_name: str = ""
    system_prompt: str = ""
    max_tokens: Optional[int] = None
    temperature: Optional[float] = None
    top_p: Optional[float] = None
    tool_choice: Optional[str] = None
    thinking_enabled: Optional[bool] = None
    tools: list[ToolDefinition] = field(default_factory=list)
    tags: dict[str, str] = field(default_factory=dict)
    metadata: dict[str, Any] = field(default_factory=dict)
    started_at: Optional[datetime] = None


@dataclass(slots=True)
class EmbeddingStart:
    """Seed fields used when embedding recording starts."""

    model: ModelRef
    agent_name: str = ""
    agent_version: str = ""
    dimensions: Optional[int] = None
    encoding_format: str = ""
    tags: dict[str, str] = field(default_factory=dict)
    metadata: dict[str, Any] = field(default_factory=dict)
    started_at: Optional[datetime] = None


@dataclass(slots=True)
class EmbeddingResult:
    """Result fields set before an embedding span is finalized."""

    input_count: int = 0
    input_tokens: int = 0
    input_texts: list[str] = field(default_factory=list)
    response_model: str = ""
    dimensions: Optional[int] = None


@dataclass(slots=True)
class Generation:
    """Final normalized generation payload exported by the SDK."""

    id: str = ""
    conversation_id: str = ""
    agent_name: str = ""
    agent_version: str = ""
    mode: Optional[GenerationMode] = None
    operation_name: str = ""
    trace_id: str = ""
    span_id: str = ""
    model: ModelRef = field(default_factory=ModelRef)
    response_id: str = ""
    response_model: str = ""
    system_prompt: str = ""
    max_tokens: Optional[int] = None
    temperature: Optional[float] = None
    top_p: Optional[float] = None
    tool_choice: Optional[str] = None
    thinking_enabled: Optional[bool] = None
    input: list[Message] = field(default_factory=list)
    output: list[Message] = field(default_factory=list)
    tools: list[ToolDefinition] = field(default_factory=list)
    usage: TokenUsage = field(default_factory=TokenUsage)
    stop_reason: str = ""
    started_at: Optional[datetime] = None
    completed_at: Optional[datetime] = None
    tags: dict[str, str] = field(default_factory=dict)
    metadata: dict[str, Any] = field(default_factory=dict)
    artifacts: list[Artifact] = field(default_factory=list)
    call_error: str = ""


@dataclass(slots=True)
class ToolExecutionStart:
    """Seed fields for execute_tool span recording."""

    tool_name: str
    tool_call_id: str = ""
    tool_type: str = ""
    tool_description: str = ""
    conversation_id: str = ""
    agent_name: str = ""
    agent_version: str = ""
    include_content: bool = False
    started_at: Optional[datetime] = None


@dataclass(slots=True)
class ToolExecutionEnd:
    """Completion payload for execute_tool span recording."""

    arguments: Any = None
    result: Any = None
    completed_at: Optional[datetime] = None


@dataclass(slots=True)
class ExportGenerationResult:
    """Per-item generation ingest result."""

    generation_id: str
    accepted: bool
    error: str = ""


@dataclass(slots=True)
class ExportGenerationsRequest:
    """Generation export request payload."""

    generations: list[Generation]


@dataclass(slots=True)
class ExportGenerationsResponse:
    """Generation export response payload."""

    results: list[ExportGenerationResult]


@dataclass(slots=True)
class ConversationRatingInput:
    """SDK input payload for submitting a conversation rating."""

    rating_id: str
    rating: ConversationRatingValue
    comment: str = ""
    metadata: dict[str, Any] = field(default_factory=dict)
    generation_id: str = ""
    rater_id: str = ""
    source: str = ""


@dataclass(slots=True)
class ConversationRating:
    """Conversation rating event returned by Sigil."""

    rating_id: str
    conversation_id: str
    rating: ConversationRatingValue
    created_at: datetime
    comment: str = ""
    metadata: dict[str, Any] = field(default_factory=dict)
    generation_id: str = ""
    rater_id: str = ""
    source: str = ""


@dataclass(slots=True)
class ConversationRatingSummary:
    """Aggregated conversation rating summary."""

    total_count: int
    good_count: int
    bad_count: int
    latest_rated_at: datetime
    has_bad_rating: bool
    latest_rating: ConversationRatingValue | None = None
    latest_bad_at: datetime | None = None


@dataclass(slots=True)
class SubmitConversationRatingResponse:
    """Conversation rating create response envelope."""

    rating: ConversationRating
    summary: ConversationRatingSummary


def utc_now() -> datetime:
    """Returns the current UTC timestamp."""

    return datetime.now(timezone.utc)


def text_part(text: str) -> Part:
    """Creates a text part."""

    return Part(kind=PartKind.TEXT, text=text)


def thinking_part(thinking: str) -> Part:
    """Creates a thinking part."""

    return Part(kind=PartKind.THINKING, thinking=thinking)


def tool_call_part(tool_call: ToolCall) -> Part:
    """Creates a tool-call part."""

    return Part(kind=PartKind.TOOL_CALL, tool_call=tool_call)


def tool_result_part(tool_result: ToolResult) -> Part:
    """Creates a tool-result part."""

    return Part(kind=PartKind.TOOL_RESULT, tool_result=tool_result)


def user_text_message(text: str) -> Message:
    """Creates a user message with one text part."""

    return Message(role=MessageRole.USER, parts=[text_part(text)])


def assistant_text_message(text: str) -> Message:
    """Creates an assistant message with one text part."""

    return Message(role=MessageRole.ASSISTANT, parts=[text_part(text)])


def tool_result_message(tool_call_id: str, content: str) -> Message:
    """Creates a tool message with one tool-result part."""

    return Message(
        role=MessageRole.TOOL,
        parts=[
            tool_result_part(
                ToolResult(
                    tool_call_id=tool_call_id,
                    content=content,
                )
            )
        ],
    )
