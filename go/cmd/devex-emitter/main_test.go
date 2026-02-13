package main

import (
	"testing"
	"time"

	"github.com/grafana/sigil/sdks/go/sigil"
)

func TestBuildTagEnvelopeIncludesRequiredContractFields(t *testing.T) {
	envelope := buildTagEnvelope(sourceOpenAI, sigil.GenerationModeSync, 2, 1)

	if envelope.tags["sigil.devex.language"] != languageName {
		t.Fatalf("expected language tag %q, got %q", languageName, envelope.tags["sigil.devex.language"])
	}
	if envelope.tags["sigil.devex.provider"] != "openai" {
		t.Fatalf("expected provider tag openai, got %q", envelope.tags["sigil.devex.provider"])
	}
	if envelope.tags["sigil.devex.source"] != "provider_wrapper" {
		t.Fatalf("expected source tag provider_wrapper, got %q", envelope.tags["sigil.devex.source"])
	}
	if envelope.tags["sigil.devex.mode"] != "SYNC" {
		t.Fatalf("expected mode tag SYNC, got %q", envelope.tags["sigil.devex.mode"])
	}

	if envelope.metadata["turn_index"] != 2 {
		t.Fatalf("expected turn_index metadata 2, got %#v", envelope.metadata["turn_index"])
	}
	if envelope.metadata["conversation_slot"] != 1 {
		t.Fatalf("expected conversation_slot metadata 1, got %#v", envelope.metadata["conversation_slot"])
	}
	if envelope.metadata["emitter"] != "sdk-traffic" {
		t.Fatalf("expected emitter metadata sdk-traffic, got %#v", envelope.metadata["emitter"])
	}
	if envelope.metadata["provider_shape"] == "" {
		t.Fatalf("expected provider_shape metadata to be set")
	}
	if envelope.agentPersona == "" {
		t.Fatalf("expected non-empty agent persona")
	}
}

func TestSourceTagForCustomProvider(t *testing.T) {
	if got := sourceTagFor(sourceCustom); got != "core_custom" {
		t.Fatalf("expected core_custom, got %q", got)
	}
	if got := sourceTagFor(sourceGemini); got != "provider_wrapper" {
		t.Fatalf("expected provider_wrapper, got %q", got)
	}
}

func TestChooseModeUsesThreshold(t *testing.T) {
	if got := chooseMode(10, 30); got != sigil.GenerationModeStream {
		t.Fatalf("expected STREAM, got %s", got)
	}
	if got := chooseMode(30, 30); got != sigil.GenerationModeSync {
		t.Fatalf("expected SYNC, got %s", got)
	}
}

func TestEnsureThreadRotatesConversationAtThreshold(t *testing.T) {
	thread := &threadState{}
	ensureThread(thread, 3, sourceOpenAI, 0)
	if thread.turn != 0 {
		t.Fatalf("expected initial turn 0, got %d", thread.turn)
	}
	if thread.conversationID == "" {
		t.Fatalf("expected conversation id to be set")
	}

	firstID := thread.conversationID
	thread.turn = 3
	// newConversationID uses Unix millis, so ensure the timestamp can advance.
	time.Sleep(2 * time.Millisecond)
	ensureThread(thread, 3, sourceOpenAI, 0)
	if thread.turn != 0 {
		t.Fatalf("expected rotated turn 0, got %d", thread.turn)
	}
	if thread.conversationID == firstID {
		t.Fatalf("expected rotated conversation id to change")
	}
}
