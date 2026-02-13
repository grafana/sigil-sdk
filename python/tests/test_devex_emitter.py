from __future__ import annotations

from importlib import util as importlib_util
from pathlib import Path
import sys
import time

MODULE_PATH = Path(__file__).resolve().parents[1] / "scripts" / "devex_emitter.py"
MODULE_SPEC = importlib_util.spec_from_file_location("devex_emitter", MODULE_PATH)
assert MODULE_SPEC is not None and MODULE_SPEC.loader is not None
emitter = importlib_util.module_from_spec(MODULE_SPEC)
sys.modules[MODULE_SPEC.name] = emitter
MODULE_SPEC.loader.exec_module(emitter)


def test_tags_and_metadata_include_required_contract_fields() -> None:
    persona, tags, metadata = emitter.build_tags_metadata("openai", "SYNC", 2, 1)

    assert persona in {"planner", "retriever", "executor"}
    assert tags["sigil.devex.language"] == "python"
    assert tags["sigil.devex.provider"] == "openai"
    assert tags["sigil.devex.source"] == "provider_wrapper"
    assert tags["sigil.devex.mode"] == "SYNC"

    assert metadata["turn_index"] == 2
    assert metadata["conversation_slot"] == 1
    assert metadata["agent_persona"] == persona
    assert metadata["emitter"] == "sdk-traffic"
    assert isinstance(metadata["provider_shape"], str)


def test_custom_provider_source_uses_core_custom_tag() -> None:
    assert emitter.source_tag_for("mistral") == "core_custom"
    assert emitter.source_tag_for("gemini") == "provider_wrapper"


def test_mode_choice_respects_stream_threshold(monkeypatch) -> None:
    monkeypatch.setattr(emitter.random, "randint", lambda _a, _b: 10)
    assert emitter.choose_mode(30) == "STREAM"

    monkeypatch.setattr(emitter.random, "randint", lambda _a, _b: 35)
    assert emitter.choose_mode(30) == "SYNC"


def test_thread_rotation_resets_turn_after_threshold() -> None:
    state = emitter.SourceState(conversations=1)

    first = emitter.resolve_thread(state, rotate_turns=3, source="openai", slot=0)
    assert first.turn == 0
    first_id = first.conversation_id
    assert first_id

    first.turn = 3
    time.sleep(0.002)
    rotated = emitter.resolve_thread(state, rotate_turns=3, source="openai", slot=0)
    assert rotated.turn == 0
    assert rotated.conversation_id != first_id
