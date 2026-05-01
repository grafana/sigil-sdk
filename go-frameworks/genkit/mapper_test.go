package genkit

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/firebase/genkit/go/ai"
	"github.com/grafana/sigil-sdk/go/sigil"
)

func TestMapRole(t *testing.T) {
	tests := []struct {
		name string
		in   ai.Role
		want sigil.Role
	}{
		{"user", ai.RoleUser, sigil.RoleUser},
		{"model", ai.RoleModel, sigil.RoleAssistant},
		{"tool", ai.RoleTool, sigil.RoleTool},
		{"system falls through to user", ai.RoleSystem, sigil.RoleUser},
		{"unknown falls through to user", ai.Role("other"), sigil.RoleUser},
		{"empty falls through to user", ai.Role(""), sigil.RoleUser},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapRole(tt.in); got != tt.want {
				t.Fatalf("mapRole(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestMapPart(t *testing.T) {
	tests := []struct {
		name   string
		in     *ai.Part
		wantOK bool
		check  func(t *testing.T, p sigil.Part)
	}{
		{
			name:   "text",
			in:     ai.NewTextPart("hello"),
			wantOK: true,
			check: func(t *testing.T, p sigil.Part) {
				if p.Kind != sigil.PartKindText {
					t.Fatalf("expected text kind, got %v", p.Kind)
				}
				if p.Text != "hello" {
					t.Fatalf("expected 'hello', got %q", p.Text)
				}
			},
		},
		{
			name: "tool request",
			in: ai.NewToolRequestPart(&ai.ToolRequest{
				Name:  "weather",
				Ref:   "call-1",
				Input: map[string]any{"city": "Paris"},
			}),
			wantOK: true,
			check: func(t *testing.T, p sigil.Part) {
				if p.Kind != sigil.PartKindToolCall {
					t.Fatalf("expected tool_call kind, got %v", p.Kind)
				}
				if p.ToolCall.Name != "weather" {
					t.Fatalf("expected tool name 'weather', got %q", p.ToolCall.Name)
				}
				if p.ToolCall.ID != "call-1" {
					t.Fatalf("expected tool call ID 'call-1', got %q", p.ToolCall.ID)
				}
				var input map[string]any
				if err := json.Unmarshal(p.ToolCall.InputJSON, &input); err != nil {
					t.Fatalf("unmarshal input: %v", err)
				}
				if input["city"] != "Paris" {
					t.Fatalf("expected city=Paris, got %v", input["city"])
				}
			},
		},
		{
			name: "tool response with output",
			in: ai.NewToolResponsePart(&ai.ToolResponse{
				Name:   "weather",
				Ref:    "call-1",
				Output: map[string]any{"temp": 18},
			}),
			wantOK: true,
			check: func(t *testing.T, p sigil.Part) {
				if p.Kind != sigil.PartKindToolResult {
					t.Fatalf("expected tool_result kind, got %v", p.Kind)
				}
				if p.ToolResult.Name != "weather" {
					t.Fatalf("expected tool name 'weather', got %q", p.ToolResult.Name)
				}
				if p.ToolResult.ToolCallID != "call-1" {
					t.Fatalf("expected tool call ID 'call-1', got %q", p.ToolResult.ToolCallID)
				}
				if len(p.ToolResult.ContentJSON) == 0 {
					t.Fatal("expected non-empty ContentJSON")
				}
			},
		},
		{
			name: "tool response with string output",
			in: ai.NewToolResponsePart(&ai.ToolResponse{
				Name:   "echo",
				Ref:    "call-2",
				Output: "hello world",
			}),
			wantOK: true,
			check: func(t *testing.T, p sigil.Part) {
				if p.ToolResult.Content != "hello world" {
					t.Fatalf("expected content 'hello world', got %q", p.ToolResult.Content)
				}
			},
		},
		{
			name: "tool response with content fallback",
			in: &ai.Part{
				Kind: ai.PartToolResponse,
				ToolResponse: &ai.ToolResponse{
					Name:    "search",
					Ref:     "call-3",
					Content: []*ai.Part{ai.NewTextPart("search results")},
				},
			},
			wantOK: true,
			check: func(t *testing.T, p sigil.Part) {
				if p.Kind != sigil.PartKindToolResult {
					t.Fatalf("expected tool_result kind, got %v", p.Kind)
				}
				if p.ToolResult.Name != "search" {
					t.Fatalf("expected tool name 'search', got %q", p.ToolResult.Name)
				}
				if len(p.ToolResult.ContentJSON) == 0 {
					t.Fatal("expected non-empty ContentJSON from Content fallback")
				}
			},
		},
		{
			name: "tool response with output takes priority over content",
			in: &ai.Part{
				Kind: ai.PartToolResponse,
				ToolResponse: &ai.ToolResponse{
					Name:    "dual",
					Ref:     "call-4",
					Output:  map[string]any{"result": "structured"},
					Content: []*ai.Part{ai.NewTextPart("text content")},
				},
			},
			wantOK: true,
			check: func(t *testing.T, p sigil.Part) {
				var output map[string]any
				if err := json.Unmarshal(p.ToolResult.ContentJSON, &output); err != nil {
					t.Fatalf("unmarshal output: %v", err)
				}
				if output["result"] != "structured" {
					t.Fatalf("expected structured output, got %v", output)
				}
			},
		},
		{
			name:   "reasoning",
			in:     ai.NewReasoningPart("thinking step", nil),
			wantOK: true,
			check: func(t *testing.T, p sigil.Part) {
				if p.Kind != sigil.PartKindThinking {
					t.Fatalf("expected thinking kind, got %v", p.Kind)
				}
				if p.Thinking != "thinking step" {
					t.Fatalf("expected 'thinking step', got %q", p.Thinking)
				}
			},
		},
		{
			name:   "media is skipped",
			in:     ai.NewMediaPart("image/png", "data:..."),
			wantOK: false,
		},
		{
			name:   "nil tool request is skipped",
			in:     &ai.Part{Kind: ai.PartToolRequest, ToolRequest: nil},
			wantOK: false,
		},
		{
			name:   "nil tool response is skipped",
			in:     &ai.Part{Kind: ai.PartToolResponse, ToolResponse: nil},
			wantOK: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := mapPart(tt.in)
			if ok != tt.wantOK {
				t.Fatalf("mapPart ok=%v, want %v", ok, tt.wantOK)
			}
			if ok && tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

func TestMapMessages(t *testing.T) {
	tests := []struct {
		name         string
		msgs         []*ai.Message
		wantMsgCount int
		wantSystem   string
		checkRoles   []sigil.Role
	}{
		{
			name:         "nil messages",
			msgs:         nil,
			wantMsgCount: 0,
			wantSystem:   "",
		},
		{
			name:         "empty messages",
			msgs:         []*ai.Message{},
			wantMsgCount: 0,
			wantSystem:   "",
		},
		{
			name: "extracts system prompt and maps messages",
			msgs: []*ai.Message{
				{Role: ai.RoleSystem, Content: []*ai.Part{ai.NewTextPart("You are a helpful assistant.")}},
				{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("Hello")}},
				{Role: ai.RoleModel, Content: []*ai.Part{ai.NewTextPart("Hi there")}},
			},
			wantMsgCount: 2,
			wantSystem:   "You are a helpful assistant.",
			checkRoles:   []sigil.Role{sigil.RoleUser, sigil.RoleAssistant},
		},
		{
			name: "no system message",
			msgs: []*ai.Message{
				{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("Hello")}},
			},
			wantMsgCount: 1,
			wantSystem:   "",
			checkRoles:   []sigil.Role{sigil.RoleUser},
		},
		{
			name: "tool role preserved",
			msgs: []*ai.Message{
				{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("Hello")}},
				{Role: ai.RoleTool, Content: []*ai.Part{ai.NewToolResponsePart(&ai.ToolResponse{Name: "t", Ref: "r", Output: "ok"})}},
			},
			wantMsgCount: 2,
			checkRoles:   []sigil.Role{sigil.RoleUser, sigil.RoleTool},
		},
		{
			name: "multiple system parts joined",
			msgs: []*ai.Message{
				{Role: ai.RoleSystem, Content: []*ai.Part{
					ai.NewTextPart("Part one."),
					ai.NewTextPart("Part two."),
				}},
			},
			wantMsgCount: 0,
			wantSystem:   "Part one.\nPart two.",
		},
		{
			name: "media-only message skipped",
			msgs: []*ai.Message{
				{Role: ai.RoleUser, Content: []*ai.Part{ai.NewMediaPart("image/png", "data:...")}},
				{Role: ai.RoleUser, Content: []*ai.Part{ai.NewTextPart("describe this")}},
			},
			wantMsgCount: 1,
			checkRoles:   []sigil.Role{sigil.RoleUser},
		},
		{
			name: "all-media messages produce empty result",
			msgs: []*ai.Message{
				{Role: ai.RoleUser, Content: []*ai.Part{ai.NewMediaPart("image/png", "data:...")}},
			},
			wantMsgCount: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mapped, systemPrompt := mapMessages(tt.msgs)
			if systemPrompt != tt.wantSystem {
				t.Fatalf("system prompt: got %q, want %q", systemPrompt, tt.wantSystem)
			}
			if len(mapped) != tt.wantMsgCount {
				t.Fatalf("message count: got %d, want %d", len(mapped), tt.wantMsgCount)
			}
			for i, role := range tt.checkRoles {
				if mapped[i].Role != role {
					t.Fatalf("message[%d] role: got %v, want %v", i, mapped[i].Role, role)
				}
			}
		})
	}
}

func TestMapUsage(t *testing.T) {
	tests := []struct {
		name string
		in   *ai.GenerationUsage
		want sigil.TokenUsage
	}{
		{
			name: "nil",
			in:   nil,
			want: sigil.TokenUsage{},
		},
		{
			name: "full",
			in: &ai.GenerationUsage{
				InputTokens:         100,
				OutputTokens:        50,
				TotalTokens:         150,
				CachedContentTokens: 20,
				ThoughtsTokens:      10,
			},
			want: sigil.TokenUsage{
				InputTokens:          100,
				OutputTokens:         50,
				TotalTokens:          150,
				CacheReadInputTokens: 20,
				ReasoningTokens:      10,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapUsage(tt.in)
			if got != tt.want {
				t.Fatalf("mapUsage() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestMapTools(t *testing.T) {
	tests := []struct {
		name  string
		tools []*ai.ToolDefinition
		check func(t *testing.T, mapped []sigil.ToolDefinition)
	}{
		{
			name:  "nil returns nil",
			tools: nil,
			check: func(t *testing.T, mapped []sigil.ToolDefinition) {
				if mapped != nil {
					t.Fatalf("expected nil, got %v", mapped)
				}
			},
		},
		{
			name:  "empty returns nil",
			tools: []*ai.ToolDefinition{},
			check: func(t *testing.T, mapped []sigil.ToolDefinition) {
				if mapped != nil {
					t.Fatalf("expected nil, got %v", mapped)
				}
			},
		},
		{
			name: "multiple tools",
			tools: []*ai.ToolDefinition{
				{Name: "weather", Description: "Get weather"},
				{Name: "search", Description: "Search web"},
			},
			check: func(t *testing.T, mapped []sigil.ToolDefinition) {
				if len(mapped) != 2 {
					t.Fatalf("expected 2 tools, got %d", len(mapped))
				}
				if mapped[0].Name != "weather" || mapped[1].Name != "search" {
					t.Fatalf("unexpected tool names: %q, %q", mapped[0].Name, mapped[1].Name)
				}
			},
		},
		{
			name: "single tool",
			tools: []*ai.ToolDefinition{
				{
					Name:        "weather",
					Description: "Get weather",
					InputSchema: map[string]any{"type": "object", "properties": map[string]any{"city": map[string]any{"type": "string"}}},
				},
			},
			check: func(t *testing.T, mapped []sigil.ToolDefinition) {
				if len(mapped) != 1 {
					t.Fatalf("expected 1 tool, got %d", len(mapped))
				}
				if mapped[0].Name != "weather" {
					t.Fatalf("expected tool name 'weather', got %q", mapped[0].Name)
				}
				if mapped[0].Description != "Get weather" {
					t.Fatalf("expected description 'Get weather', got %q", mapped[0].Description)
				}
				if len(mapped[0].InputSchema) == 0 {
					t.Fatal("expected non-empty input schema")
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.check(t, mapTools(tt.tools))
		})
	}
}

func TestParseModelRef(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		provider string
		model    string
	}{
		{"provider/model", "anthropic/claude-3.5-sonnet", "anthropic", "claude-3.5-sonnet"},
		{"just model", "gpt-5", "gpt-5", "gpt-5"},
		{"empty", "", "", ""},
		{"whitespace", "  openai/gpt-5  ", "openai", "gpt-5"},
		{"multiple slashes", "provider/org/model", "provider", "org/model"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ref := parseModelRef(tt.in)
			if ref.Provider != tt.provider {
				t.Fatalf("provider: got %q, want %q", ref.Provider, tt.provider)
			}
			if ref.Name != tt.model {
				t.Fatalf("name: got %q, want %q", ref.Name, tt.model)
			}
		})
	}
}

func TestExtractModelConfig(t *testing.T) {
	int64Ptr := func(v int64) *int64 { return &v }
	float64Ptr := func(v float64) *float64 { return &v }

	tests := []struct {
		name            string
		config          any
		wantMaxTokens   *int64
		wantTemperature *float64
		wantTopP        *float64
	}{
		{
			name:   "nil config",
			config: nil,
		},
		{
			name:   "nil pointer",
			config: (*ai.GenerationCommonConfig)(nil),
		},
		{
			name:   "unknown config type",
			config: "not a config",
		},
		{
			name: "pointer with all fields",
			config: &ai.GenerationCommonConfig{
				MaxOutputTokens: 1024,
				Temperature:     0.7,
				TopP:            0.9,
			},
			wantMaxTokens:   int64Ptr(1024),
			wantTemperature: float64Ptr(0.7),
			wantTopP:        float64Ptr(0.9),
		},
		{
			name: "value with partial fields",
			config: ai.GenerationCommonConfig{
				MaxOutputTokens: 512,
				Temperature:     0.5,
			},
			wantMaxTokens:   int64Ptr(512),
			wantTemperature: float64Ptr(0.5),
			wantTopP:        float64Ptr(0),
		},
		{
			name: "map with float64 values",
			config: map[string]any{
				"maxOutputTokens": float64(2048),
				"temperature":     float64(0.3),
				"topP":            float64(0.8),
			},
			wantMaxTokens:   int64Ptr(2048),
			wantTemperature: float64Ptr(0.3),
			wantTopP:        float64Ptr(0.8),
		},
		{
			name: "map with temperature zero",
			config: map[string]any{
				"temperature": float64(0),
			},
			wantTemperature: float64Ptr(0),
		},
		{
			name: "map with topP zero",
			config: map[string]any{
				"topP": float64(0),
			},
			wantTopP: float64Ptr(0),
		},
		{
			name: "struct with temperature zero",
			config: ai.GenerationCommonConfig{
				Temperature: 0,
			},
			wantTemperature: float64Ptr(0),
			wantTopP:        float64Ptr(0),
		},
		{
			name: "struct with topP zero",
			config: ai.GenerationCommonConfig{
				TopP: 0,
			},
			wantTemperature: float64Ptr(0),
			wantTopP:        float64Ptr(0),
		},
		{
			name: "pointer with temperature zero",
			config: &ai.GenerationCommonConfig{
				Temperature: 0,
			},
			wantTemperature: float64Ptr(0),
			wantTopP:        float64Ptr(0),
		},
		{
			name: "map with integer maxOutputTokens",
			config: map[string]any{
				"maxOutputTokens": 1024,
			},
			wantMaxTokens: int64Ptr(1024),
		},
		{
			name: "map with extra unknown keys",
			config: map[string]any{
				"temperature": float64(0.5),
				"unknownKey":  "ignored",
			},
			wantTemperature: float64Ptr(0.5),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			maxTokens, temperature, topP := extractModelConfig(tt.config)
			if !ptrEqual(maxTokens, tt.wantMaxTokens) {
				t.Fatalf("maxTokens: got %v, want %v", fmtPtr(maxTokens), fmtPtr(tt.wantMaxTokens))
			}
			if !ptrEqual(temperature, tt.wantTemperature) {
				t.Fatalf("temperature: got %v, want %v", fmtPtr(temperature), fmtPtr(tt.wantTemperature))
			}
			if !ptrEqual(topP, tt.wantTopP) {
				t.Fatalf("topP: got %v, want %v", fmtPtr(topP), fmtPtr(tt.wantTopP))
			}
		})
	}
}

func ptrEqual[T comparable](a, b *T) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

func fmtPtr[T any](p *T) string {
	if p == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%v", *p)
}

func TestMapToolChoice(t *testing.T) {
	tests := []struct {
		name string
		in   ai.ToolChoice
		want *string
	}{
		{"auto", ai.ToolChoiceAuto, strPtr("auto")},
		{"required", ai.ToolChoiceRequired, strPtr("required")},
		{"none", ai.ToolChoiceNone, strPtr("none")},
		{"empty", ai.ToolChoice(""), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapToolChoice(tt.in)
			if tt.want == nil {
				if got != nil {
					t.Fatalf("expected nil, got %q", *got)
				}
				return
			}
			if got == nil || *got != *tt.want {
				t.Fatalf("got %v, want %q", got, *tt.want)
			}
		})
	}
}

func strPtr(s string) *string { return &s }
