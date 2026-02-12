package sigil

import (
	"encoding/json"
	"time"
)

const (
	defaultOperationNameSync   = "generateText"
	defaultOperationNameStream = "streamText"
)

type GenerationMode string

const (
	GenerationModeSync   GenerationMode = "SYNC"
	GenerationModeStream GenerationMode = "STREAM"
)

// ModelRef identifies the LLM provider and model used for a generation.
type ModelRef struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

// ToolDefinition describes a callable tool visible to the model.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Type        string          `json:"type,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// Generation is the normalized, provider-agnostic generation payload.
// It can represent both request/response and streaming outcomes.
type Generation struct {
	// ID is the Sigil generation identifier. If empty, End assigns one.
	ID             string         `json:"id,omitempty"`
	ConversationID string         `json:"conversation_id,omitempty"`
	Mode           GenerationMode `json:"mode,omitempty"`
	// OperationName maps to gen_ai.operation.name.
	// Defaults are mode-aware:
	//   - SYNC   -> "generateText"
	//   - STREAM -> "streamText"
	OperationName string `json:"operation_name,omitempty"`
	// TraceID and SpanID identify the OTel span created by StartGeneration or
	// StartStreamingGeneration.
	TraceID       string            `json:"trace_id,omitempty"`
	SpanID        string            `json:"span_id,omitempty"`
	Model         ModelRef          `json:"model"`
	ResponseID    string            `json:"response_id,omitempty"`
	ResponseModel string            `json:"response_model,omitempty"`
	SystemPrompt  string            `json:"system_prompt,omitempty"`
	Input         []Message         `json:"input,omitempty"`
	Output        []Message         `json:"output,omitempty"`
	Tools         []ToolDefinition  `json:"tools,omitempty"`
	Usage         TokenUsage        `json:"usage,omitempty"`
	StopReason    string            `json:"stop_reason,omitempty"`
	StartedAt     time.Time         `json:"started_at,omitempty"`
	CompletedAt   time.Time         `json:"completed_at,omitempty"`
	Tags          map[string]string `json:"tags,omitempty"`
	Metadata      map[string]any    `json:"metadata,omitempty"`
	Artifacts     []Artifact        `json:"artifacts,omitempty"`
	// CallError captures upstream call failure text when End receives callErr.
	CallError string `json:"call_error,omitempty"`
}

// GenerationStart seeds generation fields before the provider call executes.
// Any zero-valued fields can be filled later by End.
type GenerationStart struct {
	ID             string
	ConversationID string
	Mode           GenerationMode
	OperationName  string
	Model          ModelRef
	SystemPrompt   string
	Tools          []ToolDefinition
	Tags           map[string]string
	Metadata       map[string]any
	StartedAt      time.Time
}

func (g Generation) Validate() error {
	return ValidateGeneration(g)
}

func defaultOperationNameForMode(mode GenerationMode) string {
	if mode == GenerationModeStream {
		return defaultOperationNameStream
	}
	return defaultOperationNameSync
}

func cloneGeneration(in Generation) Generation {
	return Generation{
		ID:             in.ID,
		ConversationID: in.ConversationID,
		Mode:           in.Mode,
		OperationName:  in.OperationName,
		TraceID:        in.TraceID,
		SpanID:         in.SpanID,
		Model:          in.Model,
		ResponseID:     in.ResponseID,
		ResponseModel:  in.ResponseModel,
		SystemPrompt:   in.SystemPrompt,
		Input:          cloneMessages(in.Input),
		Output:         cloneMessages(in.Output),
		Tools:          cloneTools(in.Tools),
		Usage:          in.Usage,
		StopReason:     in.StopReason,
		StartedAt:      in.StartedAt,
		CompletedAt:    in.CompletedAt,
		Tags:           cloneTags(in.Tags),
		Metadata:       cloneMetadata(in.Metadata),
		Artifacts:      cloneArtifacts(in.Artifacts),
		CallError:      in.CallError,
	}
}

func cloneGenerationStart(in GenerationStart) GenerationStart {
	return GenerationStart{
		ID:             in.ID,
		ConversationID: in.ConversationID,
		Mode:           in.Mode,
		OperationName:  in.OperationName,
		Model:          in.Model,
		SystemPrompt:   in.SystemPrompt,
		Tools:          cloneTools(in.Tools),
		Tags:           cloneTags(in.Tags),
		Metadata:       cloneMetadata(in.Metadata),
		StartedAt:      in.StartedAt,
	}
}

func cloneMessages(in []Message) []Message {
	if len(in) == 0 {
		return nil
	}

	out := make([]Message, len(in))
	for i := range in {
		out[i] = Message{
			Role:  in[i].Role,
			Name:  in[i].Name,
			Parts: cloneParts(in[i].Parts),
		}
	}

	return out
}

func cloneParts(in []Part) []Part {
	if len(in) == 0 {
		return nil
	}

	out := make([]Part, len(in))
	for i := range in {
		out[i] = Part{
			Kind:     in[i].Kind,
			Text:     in[i].Text,
			Thinking: in[i].Thinking,
			Metadata: in[i].Metadata,
		}

		if in[i].ToolCall != nil {
			call := *in[i].ToolCall
			call.InputJSON = append([]byte(nil), call.InputJSON...)
			out[i].ToolCall = &call
		}

		if in[i].ToolResult != nil {
			result := *in[i].ToolResult
			result.ContentJSON = append([]byte(nil), result.ContentJSON...)
			out[i].ToolResult = &result
		}
	}

	return out
}

func cloneTools(in []ToolDefinition) []ToolDefinition {
	if len(in) == 0 {
		return nil
	}

	out := make([]ToolDefinition, len(in))
	copy(out, in)

	for i := range out {
		out[i].InputSchema = append([]byte(nil), out[i].InputSchema...)
	}

	return out
}

func cloneArtifacts(in []Artifact) []Artifact {
	if len(in) == 0 {
		return nil
	}

	out := make([]Artifact, len(in))
	copy(out, in)

	for i := range out {
		out[i].Payload = append([]byte(nil), out[i].Payload...)
	}

	return out
}

func cloneTags(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}

	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}

	return out
}

func cloneMetadata(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}

	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}

	return out
}
