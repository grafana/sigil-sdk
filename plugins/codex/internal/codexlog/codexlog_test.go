package codexlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadSessionMetaSubagent(t *testing.T) {
	path := writeTranscript(t, `{"type":"session_meta","payload":{"id":"child","thread_source":"subagent","agent_nickname":"Dalton","agent_role":"reviewer","source":{"subagent":{"thread_spawn":{"parent_thread_id":"parent","depth":1}}}}}`)

	got, ok, err := ReadSessionMeta(path)
	if err != nil {
		t.Fatalf("ReadSessionMeta: %v", err)
	}
	if !ok {
		t.Fatal("expected session_meta")
	}
	if got.SessionID != "child" || got.ThreadSource != "subagent" || got.ParentSessionID != "parent" || got.AgentNickname != "Dalton" || got.AgentRole != "reviewer" || got.AgentDepth != 1 {
		t.Fatalf("unexpected meta: %+v", got)
	}
}

func TestReadSessionMetaOrdinarySession(t *testing.T) {
	path := writeTranscript(t, `{"type":"session_meta","payload":{"id":"parent","thread_source":"cli","agent_role":"default"}}`)

	got, ok, err := ReadSessionMeta(path)
	if err != nil {
		t.Fatalf("ReadSessionMeta: %v", err)
	}
	if !ok {
		t.Fatal("expected session_meta")
	}
	if got.SessionID != "parent" || got.ThreadSource != "cli" || got.ParentSessionID != "" {
		t.Fatalf("unexpected meta: %+v", got)
	}
}

func TestResolveSpawnLink(t *testing.T) {
	path := writeTranscript(t,
		`{"type":"session_meta","payload":{"id":"parent"}}`,
		`{"type":"turn_context","payload":{"turn_id":"turn-1"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"spawn_agent","call_id":"call_1"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"{\"agent_id\":\"child\",\"nickname\":\"Dalton\"}"}}`,
	)

	got, ok, err := ResolveSpawnLink(path, "child", func(sessionID, turnID string) string {
		return "gen:" + sessionID + ":" + turnID
	})
	if err != nil {
		t.Fatalf("ResolveSpawnLink: %v", err)
	}
	if !ok {
		t.Fatal("expected spawn link")
	}
	if got.ChildSessionID != "child" || got.ParentSessionID != "parent" || got.ParentTurnID != "turn-1" || got.ParentGenerationID != "gen:parent:turn-1" || got.SpawnCallID != "call_1" || got.AgentNickname != "Dalton" {
		t.Fatalf("unexpected link: %+v", got)
	}
}

func TestResolveSpawnLinkWithParallelCalls(t *testing.T) {
	path := writeTranscript(t,
		`{"type":"session_meta","payload":{"id":"parent"}}`,
		`{"type":"turn_context","payload":{"turn_id":"turn-1"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"spawn_agent","call_id":"call_a"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"spawn_agent","call_id":"call_b"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_a","output":{"agent_id":"other","nickname":"Ada"}}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_b","output":{"agent_id":"child","nickname":"Lin"}}}`,
	)

	got, ok, err := ResolveSpawnLink(path, "child", func(sessionID, turnID string) string {
		return "gen:" + sessionID + ":" + turnID
	})
	if err != nil {
		t.Fatalf("ResolveSpawnLink: %v", err)
	}
	if !ok {
		t.Fatal("expected spawn link")
	}
	if got.SpawnCallID != "call_b" || got.AgentNickname != "Lin" {
		t.Fatalf("unexpected link: %+v", got)
	}
}

func TestResolveSpawnLinkRequiresTurnContext(t *testing.T) {
	path := writeTranscript(t,
		`{"type":"session_meta","payload":{"id":"parent"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"spawn_agent","call_id":"call_1"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"{\"agent_id\":\"child\"}"}}`,
	)

	_, ok, err := ResolveSpawnLink(path, "child", nil)
	if err != nil {
		t.Fatalf("ResolveSpawnLink: %v", err)
	}
	if ok {
		t.Fatal("expected no link without parent turn context")
	}
}

func TestResolveSpawnLinkMalformedOutputFailsOpen(t *testing.T) {
	path := writeTranscript(t,
		`{"type":"session_meta","payload":{"id":"parent"}}`,
		`{"type":"turn_context","payload":{"turn_id":"turn-1"}}`,
		`{"type":"response_item","payload":{"type":"function_call","name":"spawn_agent","call_id":"call_1"}}`,
		`{"type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"not json"}}`,
	)

	_, ok, err := ResolveSpawnLink(path, "child", nil)
	if err != nil {
		t.Fatalf("ResolveSpawnLink: %v", err)
	}
	if ok {
		t.Fatal("expected malformed output to fail open")
	}
}

func TestReadSessionMetaRejectsOversizedLine(t *testing.T) {
	path := writeTranscript(t, strings.Repeat("x", maxLineBytes+1))

	_, _, err := ReadSessionMeta(path)
	if err == nil {
		t.Fatal("expected oversized line error")
	}
}

func writeTranscript(t *testing.T, lines ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	return path
}
