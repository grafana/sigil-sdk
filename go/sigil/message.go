package sigil

import "encoding/json"

type Role string

const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

type PartKind string

const (
	PartKindText       PartKind = "text"
	PartKindThinking   PartKind = "thinking"
	PartKindToolCall   PartKind = "tool_call"
	PartKindToolResult PartKind = "tool_result"
)

type Message struct {
	Role  Role   `json:"role"`
	Name  string `json:"name,omitempty"`
	Parts []Part `json:"parts"`
}

type Part struct {
	Kind       PartKind     `json:"kind"`
	Text       string       `json:"text,omitempty"`
	Thinking   string       `json:"thinking,omitempty"`
	ToolCall   *ToolCall    `json:"tool_call,omitempty"`
	ToolResult *ToolResult  `json:"tool_result,omitempty"`
	Metadata   PartMetadata `json:"metadata,omitempty"`
}

// PartMetadata carries provider-specific details while keeping the core shape typed.
type PartMetadata struct {
	ProviderType string `json:"provider_type,omitempty"`
}

type ToolCall struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name"`
	InputJSON json.RawMessage `json:"input_json,omitempty"`
}

type ToolResult struct {
	ToolCallID  string          `json:"tool_call_id,omitempty"`
	Name        string          `json:"name,omitempty"`
	IsError     bool            `json:"is_error,omitempty"`
	Content     string          `json:"content,omitempty"`
	ContentJSON json.RawMessage `json:"content_json,omitempty"`
}

func TextPart(text string) Part {
	return Part{
		Kind: PartKindText,
		Text: text,
	}
}

func ThinkingPart(thinking string) Part {
	return Part{
		Kind:     PartKindThinking,
		Thinking: thinking,
	}
}

func ToolCallPart(call ToolCall) Part {
	return Part{
		Kind:     PartKindToolCall,
		ToolCall: &call,
	}
}

func ToolResultPart(result ToolResult) Part {
	return Part{
		Kind:       PartKindToolResult,
		ToolResult: &result,
	}
}
