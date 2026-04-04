package mapper

import (
	"encoding/json"
	"log"
	"maps"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/grafana/sigil-sdk/go/sigil"
	"github.com/grafana/sigil-sdk/plugins/claude-code/internal/redact"
	"github.com/grafana/sigil-sdk/plugins/claude-code/internal/state"
	"github.com/grafana/sigil-sdk/plugins/claude-code/internal/transcript"
)

const (
	agentName       = "claude-code"
	maxToolInputLen = 4096
)

// Options controls how transcript lines are mapped to generations.
type Options struct {
	SessionID      string      // authoritative session ID from the hook input
	ContentCapture bool        // when true, include redacted Input/Output content
	Logger         *log.Logger // debug logger (nil = silent)
}

func (o Options) logf(format string, args ...any) {
	if o.Logger != nil {
		o.Logger.Printf(format, args...)
	}
}

type userContext struct {
	prompt      string
	toolResults []sigil.Message
}

// Coalesce merges consecutive assistant lines sharing the same RequestID
// into a single line with merged content blocks and the final line's metadata.
// Returns the coalesced lines and a safe byte offset that only covers complete
// request groups (trailing incomplete assistant groups are excluded).
func Coalesce(lines []transcript.Line) ([]transcript.Line, int64) {
	var (
		result         []transcript.Line
		pending        []transcript.Line
		lastSafeOffset int64
	)

	flush := func() {
		if len(pending) == 0 {
			return
		}
		last := pending[len(pending)-1]
		var msg transcript.AssistantMessage
		if err := json.Unmarshal(last.Message, &msg); err == nil && msg.StopReason != "" {
			merged := mergeAssistantGroup(pending)
			result = append(result, merged)
			lastSafeOffset = last.EndOffset
		}
		// Incomplete group (no terminal stop_reason): excluded,
		// offset not advanced — will be re-read next invocation.
		pending = nil
	}

	for _, line := range lines {
		if line.Type == "assistant" && line.RequestID != "" {
			if len(pending) > 0 && pending[0].RequestID != line.RequestID {
				flush()
			}
			pending = append(pending, line)
		} else {
			flush()
			result = append(result, line)
			lastSafeOffset = line.EndOffset
		}
	}
	flush()

	return result, lastSafeOffset
}

func mergeAssistantGroup(lines []transcript.Line) transcript.Line {
	if len(lines) == 1 {
		return lines[0]
	}
	final := lines[len(lines)-1]

	var allBlocks []transcript.ContentBlock
	for _, l := range lines {
		var msg transcript.AssistantMessage
		if err := json.Unmarshal(l.Message, &msg); err != nil {
			continue
		}
		allBlocks = append(allBlocks, msg.Content...)
	}

	var finalMsg transcript.AssistantMessage
	if err := json.Unmarshal(final.Message, &finalMsg); err != nil {
		return final
	}
	finalMsg.Content = allBlocks
	merged, err := json.Marshal(finalMsg)
	if err != nil {
		return final
	}
	final.Message = merged
	return final
}

// Process walks transcript lines and produces Generation records.
// It updates st.Title with the conversation title if discovered.
func Process(lines []transcript.Line, st *state.Session, opts Options, r *redact.Redactor) []sigil.Generation {
	var (
		gens []sigil.Generation
		uctx userContext
	)

	for _, line := range lines {
		switch line.Type {
		case "user":
			processUserLine(line, &uctx, st, r, opts)

		case "assistant":
			if gen, ok := processAssistantLine(line, &uctx, st, opts, r); ok {
				gens = append(gens, gen)
			}
		}
	}

	return gens
}

func processUserLine(line transcript.Line, uctx *userContext, st *state.Session, r *redact.Redactor, opts Options) {
	var msg transcript.UserMessage
	if err := json.Unmarshal(line.Message, &msg); err != nil {
		opts.logf("unmarshal user message: %v", err)
		return
	}

	text, blocks, err := transcript.ParseUserContent(msg.Content)
	if err != nil {
		opts.logf("parse user content: %v", err)
		return
	}

	if text != "" {
		uctx.prompt = text
		uctx.toolResults = nil
		if st.Title == "" {
			st.Title = text
		}
		return
	}

	var toolParts []sigil.Part
	for _, b := range blocks {
		if b.Type == "text" && b.Text != "" {
			uctx.prompt = b.Text
			uctx.toolResults = nil
			if st.Title == "" {
				st.Title = b.Text
			}
		}
		if b.Type == "tool_result" && opts.ContentCapture {
			content := b.Content()
			if r != nil {
				content = r.Redact(content)
			}
			toolParts = append(toolParts, sigil.Part{
				Kind: sigil.PartKindToolResult,
				ToolResult: &sigil.ToolResult{
					ToolCallID: b.ToolUseID,
					Content:    content,
					IsError:    b.IsError,
				},
			})
		}
	}
	if len(toolParts) > 0 {
		uctx.toolResults = []sigil.Message{{
			Role:  sigil.RoleTool,
			Parts: toolParts,
		}}
	}
}

func processAssistantLine(line transcript.Line, uctx *userContext, _ *state.Session, opts Options, r *redact.Redactor) (sigil.Generation, bool) {
	var msg transcript.AssistantMessage
	if err := json.Unmarshal(line.Message, &msg); err != nil {
		opts.logf("unmarshal assistant message: %v", err)
		return sigil.Generation{}, false
	}

	if msg.Usage.OutputTokens <= 0 {
		return sigil.Generation{}, false
	}

	isSidechain := line.IsSidechain

	completedAt, _ := time.Parse(time.RFC3339Nano, line.Timestamp)

	usage := sigil.TokenUsage{
		InputTokens:              msg.Usage.InputTokens,
		OutputTokens:             msg.Usage.OutputTokens,
		CacheReadInputTokens:     msg.Usage.CacheReadInputTokens,
		CacheCreationInputTokens: msg.Usage.CacheCreationInputTokens,
		CacheWriteInputTokens:    msg.Usage.CacheCreationInputTokens,
	}
	usage.TotalTokens = usage.InputTokens + usage.OutputTokens

	gen := sigil.Generation{
		ID:                generationID(line),
		ConversationID:    opts.SessionID,
		ConversationTitle: opts.SessionID,
		AgentName:         agentName,
		AgentVersion:      line.Version,
		Mode:              sigil.GenerationModeSync,
		OperationName:     "generateText",
		Model: sigil.ModelRef{
			Provider: "anthropic",
			Name:     msg.Model,
		},
		Usage:       usage,
		StopReason:  msg.StopReason,
		StartedAt:   completedAt, // no real start time; set equal to avoid zero-value skip in SDK metrics
		CompletedAt: completedAt,
		Tags:        buildTags(line, isSidechain),
	}

	toolNames := map[string]bool{}
	hasThinking := false

	for _, block := range msg.Content {
		switch block.Type {
		case "tool_use":
			toolNames[block.Name] = true
		case "thinking":
			hasThinking = true
		}
	}

	if len(toolNames) > 0 {
		gen.Tools = buildToolDefs(toolNames)
	}

	if hasThinking {
		gen.ThinkingEnabled = ptrBool(true)
	}

	if opts.ContentCapture {
		gen.Input = buildInput(uctx, r)
		gen.Output = buildOutput(msg.Content, r)
	} else {
		gen.Output = buildOutputRedacted(msg.Content)
	}

	return gen, true
}

func buildTags(line transcript.Line, subagent bool) map[string]string {
	if line.GitBranch == "" && line.CWD == "" && line.Entrypoint == "" && !subagent {
		return nil
	}
	tags := make(map[string]string, 4)
	if line.GitBranch != "" {
		tags["git.branch"] = line.GitBranch
	}
	if line.CWD != "" {
		tags["cwd"] = line.CWD
	}
	if line.Entrypoint != "" {
		tags["entrypoint"] = line.Entrypoint
	}
	if subagent {
		tags["subagent"] = "true"
	}
	return tags
}

func buildToolDefs(names map[string]bool) []sigil.ToolDefinition {
	sorted := slices.Sorted(maps.Keys(names))
	defs := make([]sigil.ToolDefinition, len(sorted))
	for i, name := range sorted {
		defs[i] = sigil.ToolDefinition{Name: name, Type: "function"}
	}
	return defs
}

func buildInput(uctx *userContext, r *redact.Redactor) []sigil.Message {
	if len(uctx.toolResults) > 0 {
		return uctx.toolResults
	}
	if uctx.prompt == "" {
		return nil
	}
	text := uctx.prompt
	if r != nil {
		text = r.RedactLightweight(text)
	}
	return []sigil.Message{{
		Role: sigil.RoleUser,
		Parts: []sigil.Part{{
			Kind: sigil.PartKindText,
			Text: text,
		}},
	}}
}

func buildOutput(blocks []transcript.ContentBlock, r *redact.Redactor) []sigil.Message {
	var parts []sigil.Part

	for _, block := range blocks {
		switch block.Type {
		case "text":
			text := block.Text
			if r != nil {
				text = r.RedactLightweight(text)
			}
			parts = append(parts, sigil.Part{
				Kind: sigil.PartKindText,
				Text: text,
			})

		case "thinking":
			// Omit content (can be 50KB+), just note presence
			parts = append(parts, sigil.Part{
				Kind:     sigil.PartKindThinking,
				Thinking: "[thinking block omitted]",
			})

		case "tool_use":
			inputJSON := truncateJSON(block.Input, maxToolInputLen, r)
			parts = append(parts, sigil.Part{
				Kind: sigil.PartKindToolCall,
				ToolCall: &sigil.ToolCall{
					ID:        block.ID,
					Name:      block.Name,
					InputJSON: inputJSON,
				},
			})
		}
	}

	if len(parts) == 0 {
		return nil
	}

	return []sigil.Message{{
		Role:  sigil.RoleAssistant,
		Parts: parts,
	}}
}

// buildOutputRedacted builds output with tool call structure preserved
// but all content replaced with [redacted]. Used when content capture is off.
func buildOutputRedacted(blocks []transcript.ContentBlock) []sigil.Message {
	var parts []sigil.Part

	for _, block := range blocks {
		switch block.Type {
		case "text":
			parts = append(parts, sigil.Part{
				Kind: sigil.PartKindText,
				Text: "[redacted]",
			})
		case "thinking":
			parts = append(parts, sigil.Part{
				Kind:     sigil.PartKindThinking,
				Thinking: "[redacted]",
			})
		case "tool_use":
			parts = append(parts, sigil.Part{
				Kind: sigil.PartKindToolCall,
				ToolCall: &sigil.ToolCall{
					ID:        block.ID,
					Name:      block.Name,
					InputJSON: json.RawMessage(`"[redacted]"`),
				},
			})
		}
	}

	if len(parts) == 0 {
		return nil
	}

	return []sigil.Message{{
		Role:  sigil.RoleAssistant,
		Parts: parts,
	}}
}

// truncateJSON redacts and truncates tool input JSON.
// Uses Tier 1 only (RedactLightweight) to avoid Tier 2 patterns mangling
// JSON structure. When truncation occurs, the result is wrapped as a JSON
// string (type changes from the original object/array to string).
func truncateJSON(raw json.RawMessage, maxLen int, r *redact.Redactor) json.RawMessage {
	if len(raw) == 0 {
		return raw
	}

	s := string(raw)
	if r != nil {
		s = r.RedactLightweight(s)
	}

	if len(s) <= maxLen {
		return json.RawMessage(s)
	}

	// Truncate to valid UTF-8 boundary
	truncated := s[:maxLen]
	for !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}

	quoted, _ := json.Marshal(truncated + " [truncated]")
	return json.RawMessage(quoted)
}

// generationID produces a deterministic UUID v5 from transcript data.
// Uses RequestID when available (shared across streaming fragments),
// falling back to UUID for backward compatibility.
func generationID(line transcript.Line) string {
	key := line.RequestID
	if key == "" {
		key = line.UUID
	}
	name := line.SessionID + ":" + key
	return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(name)).String()
}

func ptrBool(b bool) *bool { return &b }
