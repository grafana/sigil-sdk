"""Context helpers for conversation and agent identity propagation."""

from __future__ import annotations

import contextvars
from collections.abc import Iterator
from contextlib import contextmanager

_conversation_id: contextvars.ContextVar[str | None] = contextvars.ContextVar("sigil_conversation_id", default=None)
_conversation_title: contextvars.ContextVar[str | None] = contextvars.ContextVar(
    "sigil_conversation_title", default=None
)
_user_id: contextvars.ContextVar[str | None] = contextvars.ContextVar("sigil_user_id", default=None)
_agent_name: contextvars.ContextVar[str | None] = contextvars.ContextVar("sigil_agent_name", default=None)
_agent_version: contextvars.ContextVar[str | None] = contextvars.ContextVar("sigil_agent_version", default=None)


@contextmanager
def with_conversation_id(conversation_id: str) -> Iterator[None]:
    """Sets conversation id within a context block."""

    token = _conversation_id.set(conversation_id)
    try:
        yield
    finally:
        _conversation_id.reset(token)


@contextmanager
def with_conversation_title(conversation_title: str) -> Iterator[None]:
    """Sets conversation title within a context block."""

    token = _conversation_title.set(conversation_title)
    try:
        yield
    finally:
        _conversation_title.reset(token)


@contextmanager
def with_user_id(user_id: str) -> Iterator[None]:
    """Sets user id within a context block."""

    token = _user_id.set(user_id)
    try:
        yield
    finally:
        _user_id.reset(token)


@contextmanager
def with_agent_name(agent_name: str) -> Iterator[None]:
    """Sets agent name within a context block."""

    token = _agent_name.set(agent_name)
    try:
        yield
    finally:
        _agent_name.reset(token)


@contextmanager
def with_agent_version(agent_version: str) -> Iterator[None]:
    """Sets agent version within a context block."""

    token = _agent_version.set(agent_version)
    try:
        yield
    finally:
        _agent_version.reset(token)


def conversation_id_from_context() -> str | None:
    """Returns the current conversation id from context variables."""

    return _conversation_id.get()


def agent_name_from_context() -> str | None:
    """Returns the current agent name from context variables."""

    return _agent_name.get()


def agent_version_from_context() -> str | None:
    """Returns the current agent version from context variables."""

    return _agent_version.get()


def conversation_title_from_context() -> str | None:
    """Returns the current conversation title from context variables."""

    return _conversation_title.get()


def user_id_from_context() -> str | None:
    """Returns the current user id from context variables."""

    return _user_id.get()
