package anthropic

import (
	"errors"

	asdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/grafana/sigil/sdks/go/sigil"
)

// StreamSummary captures Anthropic stream events and an optional final message.
type StreamSummary struct {
	Events       []asdk.BetaRawMessageStreamEventUnion
	FinalMessage *asdk.BetaMessage
}

// FromStream maps Anthropic streaming output to sigil.Generation.
func FromStream(req asdk.BetaMessageNewParams, summary StreamSummary, opts ...Option) (sigil.Generation, error) {
	if summary.FinalMessage != nil {
		generation, err := FromRequestResponse(req, summary.FinalMessage, opts...)
		if err != nil {
			return sigil.Generation{}, err
		}
		return appendStreamEventsArtifact(generation, summary.Events, opts)
	}

	if len(summary.Events) == 0 {
		return sigil.Generation{}, errors.New("stream summary has no events and no final message")
	}

	options := applyOptions(opts)
	maxTokens, temperature, topP, toolChoice, thinkingEnabled, thinkingBudget := mapRequestControls(req)

	assistantParts := make([]sigil.Part, 0, len(summary.Events))
	toolParts := make([]sigil.Part, 0, 1)
	usage := sigil.TokenUsage{}
	stopReason := ""
	modelName := string(req.Model)
	responseID := ""

	for _, event := range summary.Events {
		switch event.Type {
		case "message_start":
			if event.Message.ID != "" {
				responseID = event.Message.ID
			}
			if event.Message.Model != "" {
				modelName = string(event.Message.Model)
			}
			for _, message := range mapResponseMessages(event.Message.Content) {
				if message.Role == sigil.RoleTool {
					toolParts = append(toolParts, message.Parts...)
					continue
				}
				assistantParts = append(assistantParts, message.Parts...)
			}
		case "content_block_start":
			part, ok := mapRawContentBlock(event.ContentBlock)
			if !ok {
				continue
			}
			if part.Kind == sigil.PartKindToolResult {
				toolParts = append(toolParts, part)
				continue
			}
			assistantParts = append(assistantParts, part)
		case "message_delta":
			usage = mapDeltaUsage(event.Usage)
			if event.Delta.StopReason != "" {
				stopReason = string(event.Delta.StopReason)
			}
		}
	}

	input := mapRequestMessages(req.Messages)
	output := make([]sigil.Message, 0, 2)
	if len(assistantParts) > 0 {
		output = append(output, sigil.Message{
			Role:  sigil.RoleAssistant,
			Parts: assistantParts,
		})
	}
	if len(toolParts) > 0 {
		output = append(output, sigil.Message{
			Role:  sigil.RoleTool,
			Parts: toolParts,
		})
	}

	artifacts := make([]sigil.Artifact, 0, 4)
	if options.includeRequestArtifact {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindRequest, "anthropic.request", req)
		if err != nil {
			return sigil.Generation{}, err
		}
		artifacts = append(artifacts, artifact)
	}
	if options.includeToolsArtifact && len(req.Tools) > 0 {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindTools, "anthropic.tools", req.Tools)
		if err != nil {
			return sigil.Generation{}, err
		}
		artifacts = append(artifacts, artifact)
	}
	if options.includeEventsArtifact {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindProviderEvent, "anthropic.stream_events", summary.Events)
		if err != nil {
			return sigil.Generation{}, err
		}
		artifacts = append(artifacts, artifact)
	}

	generation := sigil.Generation{
		ConversationID:  options.conversationID,
		AgentName:       options.agentName,
		AgentVersion:    options.agentVersion,
		Model:           sigil.ModelRef{Provider: options.providerName, Name: string(req.Model)},
		ResponseID:      responseID,
		ResponseModel:   modelName,
		SystemPrompt:    mapSystemPrompt(req.System),
		Input:           input,
		Output:          output,
		Tools:           mapTools(req.Tools),
		MaxTokens:       maxTokens,
		Temperature:     temperature,
		TopP:            topP,
		ToolChoice:      toolChoice,
		ThinkingEnabled: thinkingEnabled,
		Usage:           usage,
		StopReason:      stopReason,
		Tags:            cloneStringMap(options.tags),
		Metadata:        mergeThinkingBudgetMetadata(options.metadata, thinkingBudget),
		Artifacts:       artifacts,
	}

	if err := generation.Validate(); err != nil {
		return sigil.Generation{}, err
	}

	return generation, nil
}

func appendStreamEventsArtifact(generation sigil.Generation, events []asdk.BetaRawMessageStreamEventUnion, opts []Option) (sigil.Generation, error) {
	if len(events) == 0 {
		return generation, nil
	}

	options := applyOptions(opts)
	if !options.includeEventsArtifact {
		return generation, nil
	}

	artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindProviderEvent, "anthropic.stream_events", events)
	if err != nil {
		return sigil.Generation{}, err
	}

	generation.Artifacts = append(generation.Artifacts, artifact)
	return generation, nil
}

func mapRawContentBlock(block asdk.BetaRawContentBlockStartEventContentBlockUnion) (sigil.Part, bool) {
	switch block.Type {
	case "text":
		if block.Text == "" {
			return sigil.Part{}, false
		}
		return sigil.TextPart(block.Text), true
	case "thinking":
		part := sigil.ThinkingPart(block.Thinking)
		part.Metadata.ProviderType = block.Type
		return part, true
	case "redacted_thinking":
		part := sigil.ThinkingPart(block.Data)
		part.Metadata.ProviderType = block.Type
		return part, true
	case "tool_use", "server_tool_use", "mcp_tool_use":
		inputJSON, _ := marshalAny(block.Input)
		part := sigil.ToolCallPart(sigil.ToolCall{
			ID:        block.ID,
			Name:      block.Name,
			InputJSON: inputJSON,
		})
		part.Metadata.ProviderType = block.Type
		return part, true
	case "tool_result",
		"web_search_tool_result",
		"web_fetch_tool_result",
		"code_execution_tool_result",
		"bash_code_execution_tool_result",
		"text_editor_code_execution_tool_result",
		"tool_search_tool_result",
		"mcp_tool_result":
		contentJSON, _ := marshalAny(block.Content)
		part := sigil.ToolResultPart(sigil.ToolResult{
			ToolCallID:  block.ToolUseID,
			IsError:     block.IsError,
			ContentJSON: contentJSON,
		})
		part.Metadata.ProviderType = block.Type
		return part, true
	default:
		return sigil.Part{}, false
	}
}
