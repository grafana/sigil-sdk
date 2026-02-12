package sigil

import (
	"encoding/json"
	"fmt"

	sigilv1 "github.com/grafana/sigil/sdks/go/sigil/internal/gen/sigil/v1"
	"google.golang.org/protobuf/types/known/structpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func generationToProto(g Generation) (*sigilv1.Generation, error) {
	metadata, err := mapMetadataToStruct(g.Metadata)
	if err != nil {
		return nil, fmt.Errorf("map metadata: %w", err)
	}

	out := &sigilv1.Generation{
		Id:             g.ID,
		ConversationId: g.ConversationID,
		OperationName:  g.OperationName,
		Mode:           mapGenerationModeToProto(g.Mode),
		TraceId:        g.TraceID,
		SpanId:         g.SpanID,
		Model: &sigilv1.ModelRef{
			Provider: g.Model.Provider,
			Name:     g.Model.Name,
		},
		ResponseId:    g.ResponseID,
		ResponseModel: g.ResponseModel,
		SystemPrompt:  g.SystemPrompt,
		Input:         mapMessagesToProto(g.Input),
		Output:        mapMessagesToProto(g.Output),
		Tools:         mapToolsToProto(g.Tools),
		Usage:         mapUsageToProto(g.Usage),
		StopReason:    g.StopReason,
		Tags:          cloneTags(g.Tags),
		Metadata:      metadata,
		RawArtifacts:  mapArtifactsToProto(g.Artifacts),
		CallError:     g.CallError,
	}

	if !g.StartedAt.IsZero() {
		out.StartedAt = timestamppb.New(g.StartedAt)
	}
	if !g.CompletedAt.IsZero() {
		out.CompletedAt = timestamppb.New(g.CompletedAt)
	}

	return out, nil
}

func mapMetadataToStruct(metadata map[string]any) (*structpb.Struct, error) {
	if len(metadata) == 0 {
		return nil, nil
	}

	encoded, err := json.Marshal(metadata)
	if err != nil {
		return nil, err
	}

	normalized := map[string]any{}
	if err := json.Unmarshal(encoded, &normalized); err != nil {
		return nil, err
	}

	return structpb.NewStruct(normalized)
}

func mapGenerationModeToProto(mode GenerationMode) sigilv1.GenerationMode {
	switch mode {
	case GenerationModeStream:
		return sigilv1.GenerationMode_GENERATION_MODE_STREAM
	case GenerationModeSync:
		return sigilv1.GenerationMode_GENERATION_MODE_SYNC
	default:
		return sigilv1.GenerationMode_GENERATION_MODE_UNSPECIFIED
	}
}

func mapMessagesToProto(messages []Message) []*sigilv1.Message {
	if len(messages) == 0 {
		return nil
	}

	out := make([]*sigilv1.Message, 0, len(messages))
	for i := range messages {
		out = append(out, &sigilv1.Message{
			Role:  mapRoleToProto(messages[i].Role),
			Name:  messages[i].Name,
			Parts: mapPartsToProto(messages[i].Parts),
		})
	}

	return out
}

func mapRoleToProto(role Role) sigilv1.MessageRole {
	switch role {
	case RoleUser:
		return sigilv1.MessageRole_MESSAGE_ROLE_USER
	case RoleAssistant:
		return sigilv1.MessageRole_MESSAGE_ROLE_ASSISTANT
	case RoleTool:
		return sigilv1.MessageRole_MESSAGE_ROLE_TOOL
	default:
		return sigilv1.MessageRole_MESSAGE_ROLE_UNSPECIFIED
	}
}

func mapPartsToProto(parts []Part) []*sigilv1.Part {
	if len(parts) == 0 {
		return nil
	}

	out := make([]*sigilv1.Part, 0, len(parts))
	for i := range parts {
		part := &sigilv1.Part{}
		if providerType := parts[i].Metadata.ProviderType; providerType != "" {
			part.Metadata = &sigilv1.PartMetadata{ProviderType: providerType}
		}

		switch parts[i].Kind {
		case PartKindText:
			part.Payload = &sigilv1.Part_Text{Text: parts[i].Text}
		case PartKindThinking:
			part.Payload = &sigilv1.Part_Thinking{Thinking: parts[i].Thinking}
		case PartKindToolCall:
			if parts[i].ToolCall == nil {
				continue
			}
			part.Payload = &sigilv1.Part_ToolCall{ToolCall: &sigilv1.ToolCall{
				Id:        parts[i].ToolCall.ID,
				Name:      parts[i].ToolCall.Name,
				InputJson: append([]byte(nil), parts[i].ToolCall.InputJSON...),
			}}
		case PartKindToolResult:
			if parts[i].ToolResult == nil {
				continue
			}
			part.Payload = &sigilv1.Part_ToolResult{ToolResult: &sigilv1.ToolResult{
				ToolCallId:  parts[i].ToolResult.ToolCallID,
				Name:        parts[i].ToolResult.Name,
				Content:     parts[i].ToolResult.Content,
				ContentJson: append([]byte(nil), parts[i].ToolResult.ContentJSON...),
				IsError:     parts[i].ToolResult.IsError,
			}}
		}

		out = append(out, part)
	}
	return out
}

func mapToolsToProto(tools []ToolDefinition) []*sigilv1.ToolDefinition {
	if len(tools) == 0 {
		return nil
	}

	out := make([]*sigilv1.ToolDefinition, 0, len(tools))
	for i := range tools {
		out = append(out, &sigilv1.ToolDefinition{
			Name:            tools[i].Name,
			Description:     tools[i].Description,
			Type:            tools[i].Type,
			InputSchemaJson: append([]byte(nil), tools[i].InputSchema...),
		})
	}
	return out
}

func mapUsageToProto(usage TokenUsage) *sigilv1.TokenUsage {
	return &sigilv1.TokenUsage{
		InputTokens:           usage.InputTokens,
		OutputTokens:          usage.OutputTokens,
		TotalTokens:           usage.TotalTokens,
		CacheReadInputTokens:  usage.CacheReadInputTokens,
		CacheWriteInputTokens: usage.CacheWriteInputTokens,
		ReasoningTokens:       usage.ReasoningTokens,
	}
}

func mapArtifactsToProto(artifacts []Artifact) []*sigilv1.Artifact {
	if len(artifacts) == 0 {
		return nil
	}

	out := make([]*sigilv1.Artifact, 0, len(artifacts))
	for i := range artifacts {
		out = append(out, &sigilv1.Artifact{
			Kind:        mapArtifactKindToProto(artifacts[i].Kind),
			Name:        artifacts[i].Name,
			ContentType: artifacts[i].ContentType,
			Payload:     append([]byte(nil), artifacts[i].Payload...),
			RecordId:    artifacts[i].RecordID,
			Uri:         artifacts[i].URI,
		})
	}
	return out
}

func mapArtifactKindToProto(kind ArtifactKind) sigilv1.ArtifactKind {
	switch kind {
	case ArtifactKindRequest:
		return sigilv1.ArtifactKind_ARTIFACT_KIND_REQUEST
	case ArtifactKindResponse:
		return sigilv1.ArtifactKind_ARTIFACT_KIND_RESPONSE
	case ArtifactKindTools:
		return sigilv1.ArtifactKind_ARTIFACT_KIND_TOOLS
	case ArtifactKindProviderEvent:
		return sigilv1.ArtifactKind_ARTIFACT_KIND_PROVIDER_EVENT
	default:
		return sigilv1.ArtifactKind_ARTIFACT_KIND_UNSPECIFIED
	}
}
