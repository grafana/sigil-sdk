package sigil

import (
	"encoding/json"
	"time"
)

type ModelRef struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
}

type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Type        string          `json:"type,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type Generation struct {
	ID           string            `json:"id,omitempty"`
	ThreadID     string            `json:"thread_id,omitempty"`
	Model        ModelRef          `json:"model"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	Messages     []Message         `json:"messages,omitempty"`
	Tools        []ToolDefinition  `json:"tools,omitempty"`
	Usage        TokenUsage        `json:"usage,omitempty"`
	StopReason   string            `json:"stop_reason,omitempty"`
	StartedAt    time.Time         `json:"started_at,omitempty"`
	CompletedAt  time.Time         `json:"completed_at,omitempty"`
	Tags         map[string]string `json:"tags,omitempty"`
	Metadata     map[string]any    `json:"metadata,omitempty"`
	Artifacts    []Artifact        `json:"artifacts,omitempty"`
	CallError    string            `json:"call_error,omitempty"`
}

type GenerationStart struct {
	ID           string
	ThreadID     string
	Model        ModelRef
	SystemPrompt string
	Messages     []Message
	Tools        []ToolDefinition
	StartedAt    time.Time
	Tags         map[string]string
	Metadata     map[string]any
}

type GenerationRef struct {
	GenerationID string        `json:"generation_id"`
	ArtifactRefs []ArtifactRef `json:"artifact_refs,omitempty"`
}

func (g Generation) Validate() error {
	return ValidateGeneration(g)
}

func (s GenerationStart) toGeneration() Generation {
	return Generation{
		ID:           s.ID,
		ThreadID:     s.ThreadID,
		Model:        s.Model,
		SystemPrompt: s.SystemPrompt,
		Messages:     cloneMessages(s.Messages),
		Tools:        cloneTools(s.Tools),
		StartedAt:    s.StartedAt,
		Tags:         cloneTags(s.Tags),
		Metadata:     cloneMetadata(s.Metadata),
	}
}

func cloneGeneration(in Generation) Generation {
	return Generation{
		ID:           in.ID,
		ThreadID:     in.ThreadID,
		Model:        in.Model,
		SystemPrompt: in.SystemPrompt,
		Messages:     cloneMessages(in.Messages),
		Tools:        cloneTools(in.Tools),
		Usage:        in.Usage,
		StopReason:   in.StopReason,
		StartedAt:    in.StartedAt,
		CompletedAt:  in.CompletedAt,
		Tags:         cloneTags(in.Tags),
		Metadata:     cloneMetadata(in.Metadata),
		Artifacts:    cloneArtifacts(in.Artifacts),
		CallError:    in.CallError,
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
