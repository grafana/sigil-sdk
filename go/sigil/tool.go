package sigil

import "time"

// ToolExecutionStart seeds a tool execution span before the tool call runs.
type ToolExecutionStart struct {
	ToolName        string
	ToolCallID      string
	ToolType        string
	ToolDescription string
	ConversationID  string
	StartedAt       time.Time
	// IncludeContent enables gen_ai.tool.call.arguments and gen_ai.tool.call.result attributes.
	IncludeContent bool
}

// ToolExecutionEnd finalizes tool execution span attributes.
type ToolExecutionEnd struct {
	Arguments   any
	Result      any
	CompletedAt time.Time
}
