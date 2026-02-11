package anthropic

import (
	"encoding/json"
	"errors"
	"strings"

	asdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/grafana/sigil/sdks/go/sigil"
)

// FromRequestResponse maps an Anthropic request/response pair to sigil.Generation.
func FromRequestResponse(req asdk.BetaMessageNewParams, resp *asdk.BetaMessage, opts ...Option) (sigil.Generation, error) {
	if resp == nil {
		return sigil.Generation{}, errors.New("response is required")
	}

	options := applyOptions(opts)

	input := mapRequestMessages(req.Messages)
	output := mapResponseMessages(resp.Content)

	artifacts := make([]sigil.Artifact, 0, 3)
	if options.includeRequestArtifact {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindRequest, "anthropic.request", req)
		if err != nil {
			return sigil.Generation{}, err
		}
		artifacts = append(artifacts, artifact)
	}
	if options.includeResponseArtifact {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindResponse, "anthropic.response", resp)
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

	requestModel := string(req.Model)
	responseModel := string(resp.Model)
	if responseModel == "" {
		responseModel = requestModel
	}

	generation := sigil.Generation{
		ConversationID: options.conversationID,
		Model:          sigil.ModelRef{Provider: options.providerName, Name: requestModel},
		ResponseID:     resp.ID,
		ResponseModel:  responseModel,
		SystemPrompt:   mapSystemPrompt(req.System),
		Input:          input,
		Output:         output,
		Tools:          mapTools(req.Tools),
		Usage:          mapUsage(resp.Usage),
		StopReason:     string(resp.StopReason),
		Tags:           cloneStringMap(options.tags),
		Metadata:       cloneAnyMap(options.metadata),
		Artifacts:      artifacts,
	}

	if err := generation.Validate(); err != nil {
		return sigil.Generation{}, err
	}

	return generation, nil
}

func mapRequestMessages(messages []asdk.BetaMessageParam) []sigil.Message {
	if len(messages) == 0 {
		return nil
	}

	out := make([]sigil.Message, 0, len(messages))
	for i := range messages {
		role := mapRequestRole(messages[i].Role)
		normalParts := make([]sigil.Part, 0, len(messages[i].Content))
		toolParts := make([]sigil.Part, 0, 1)

		for _, block := range messages[i].Content {
			part, ok := mapRequestBlock(block)
			if !ok {
				continue
			}
			if part.Kind == sigil.PartKindToolResult {
				toolParts = append(toolParts, part)
				continue
			}
			normalParts = append(normalParts, part)
		}

		if len(normalParts) > 0 {
			out = append(out, sigil.Message{
				Role:  role,
				Parts: normalParts,
			})
		}
		if len(toolParts) > 0 {
			out = append(out, sigil.Message{
				Role:  sigil.RoleTool,
				Parts: toolParts,
			})
		}
	}

	return out
}

func mapResponseMessages(content []asdk.BetaContentBlockUnion) []sigil.Message {
	if len(content) == 0 {
		return nil
	}

	assistantParts := make([]sigil.Part, 0, len(content))
	toolParts := make([]sigil.Part, 0, 1)

	for _, block := range content {
		part, ok := mapResponseBlock(block)
		if !ok {
			continue
		}
		if part.Kind == sigil.PartKindToolResult {
			toolParts = append(toolParts, part)
			continue
		}
		assistantParts = append(assistantParts, part)
	}

	out := make([]sigil.Message, 0, 2)
	if len(assistantParts) > 0 {
		out = append(out, sigil.Message{
			Role:  sigil.RoleAssistant,
			Parts: assistantParts,
		})
	}
	if len(toolParts) > 0 {
		out = append(out, sigil.Message{
			Role:  sigil.RoleTool,
			Parts: toolParts,
		})
	}

	return out
}

func mapRequestBlock(block asdk.BetaContentBlockParamUnion) (sigil.Part, bool) {
	if block.OfText != nil {
		text := strings.TrimSpace(block.OfText.Text)
		if text == "" {
			return sigil.Part{}, false
		}
		return sigil.TextPart(text), true
	}
	if block.OfThinking != nil {
		part := sigil.ThinkingPart(block.OfThinking.Thinking)
		part.Metadata.ProviderType = "thinking"
		return part, true
	}
	if block.OfRedactedThinking != nil {
		part := sigil.ThinkingPart(block.OfRedactedThinking.Data)
		part.Metadata.ProviderType = "redacted_thinking"
		return part, true
	}
	if block.OfToolUse != nil {
		inputJSON, _ := marshalAny(block.OfToolUse.Input)
		part := sigil.ToolCallPart(sigil.ToolCall{
			ID:        block.OfToolUse.ID,
			Name:      block.OfToolUse.Name,
			InputJSON: inputJSON,
		})
		part.Metadata.ProviderType = "tool_use"
		return part, true
	}
	if block.OfServerToolUse != nil {
		inputJSON, _ := marshalAny(block.OfServerToolUse.Input)
		part := sigil.ToolCallPart(sigil.ToolCall{
			ID:        block.OfServerToolUse.ID,
			Name:      string(block.OfServerToolUse.Name),
			InputJSON: inputJSON,
		})
		part.Metadata.ProviderType = "server_tool_use"
		return part, true
	}
	if block.OfMCPToolUse != nil {
		inputJSON, _ := marshalAny(block.OfMCPToolUse.Input)
		part := sigil.ToolCallPart(sigil.ToolCall{
			ID:        block.OfMCPToolUse.ID,
			Name:      block.OfMCPToolUse.Name,
			InputJSON: inputJSON,
		})
		part.Metadata.ProviderType = "mcp_tool_use"
		return part, true
	}
	if block.OfToolResult != nil {
		contentJSON, _ := marshalAny(block.OfToolResult.Content)
		part := sigil.ToolResultPart(sigil.ToolResult{
			ToolCallID:  block.OfToolResult.ToolUseID,
			IsError:     block.OfToolResult.IsError.Value,
			ContentJSON: contentJSON,
		})
		part.Metadata.ProviderType = "tool_result"
		return part, true
	}
	if block.OfWebSearchToolResult != nil {
		contentJSON, _ := marshalAny(block.OfWebSearchToolResult.Content)
		part := sigil.ToolResultPart(sigil.ToolResult{
			ToolCallID:  block.OfWebSearchToolResult.ToolUseID,
			ContentJSON: contentJSON,
		})
		part.Metadata.ProviderType = "web_search_tool_result"
		return part, true
	}
	if block.OfWebFetchToolResult != nil {
		contentJSON, _ := marshalAny(block.OfWebFetchToolResult.Content)
		part := sigil.ToolResultPart(sigil.ToolResult{
			ToolCallID:  block.OfWebFetchToolResult.ToolUseID,
			ContentJSON: contentJSON,
		})
		part.Metadata.ProviderType = "web_fetch_tool_result"
		return part, true
	}
	if block.OfCodeExecutionToolResult != nil {
		contentJSON, _ := marshalAny(block.OfCodeExecutionToolResult.Content)
		part := sigil.ToolResultPart(sigil.ToolResult{
			ToolCallID:  block.OfCodeExecutionToolResult.ToolUseID,
			ContentJSON: contentJSON,
		})
		part.Metadata.ProviderType = "code_execution_tool_result"
		return part, true
	}
	if block.OfBashCodeExecutionToolResult != nil {
		contentJSON, _ := marshalAny(block.OfBashCodeExecutionToolResult.Content)
		part := sigil.ToolResultPart(sigil.ToolResult{
			ToolCallID:  block.OfBashCodeExecutionToolResult.ToolUseID,
			ContentJSON: contentJSON,
		})
		part.Metadata.ProviderType = "bash_code_execution_tool_result"
		return part, true
	}
	if block.OfTextEditorCodeExecutionToolResult != nil {
		contentJSON, _ := marshalAny(block.OfTextEditorCodeExecutionToolResult.Content)
		part := sigil.ToolResultPart(sigil.ToolResult{
			ToolCallID:  block.OfTextEditorCodeExecutionToolResult.ToolUseID,
			ContentJSON: contentJSON,
		})
		part.Metadata.ProviderType = "text_editor_code_execution_tool_result"
		return part, true
	}
	if block.OfToolSearchToolResult != nil {
		contentJSON, _ := marshalAny(block.OfToolSearchToolResult.Content)
		part := sigil.ToolResultPart(sigil.ToolResult{
			ToolCallID:  block.OfToolSearchToolResult.ToolUseID,
			ContentJSON: contentJSON,
		})
		part.Metadata.ProviderType = "tool_search_tool_result"
		return part, true
	}
	if block.OfMCPToolResult != nil {
		contentJSON, _ := marshalAny(block.OfMCPToolResult.Content)
		part := sigil.ToolResultPart(sigil.ToolResult{
			ToolCallID:  block.OfMCPToolResult.ToolUseID,
			IsError:     block.OfMCPToolResult.IsError.Valid() && block.OfMCPToolResult.IsError.Value,
			ContentJSON: contentJSON,
		})
		part.Metadata.ProviderType = "mcp_tool_result"
		return part, true
	}

	typ := derefString(block.GetType())
	switch typ {
	case "text":
		text := strings.TrimSpace(derefString(block.GetText()))
		if text == "" {
			return sigil.Part{}, false
		}
		return sigil.TextPart(text), true
	case "thinking":
		part := sigil.ThinkingPart(derefString(block.GetThinking()))
		part.Metadata.ProviderType = typ
		return part, true
	case "redacted_thinking":
		part := sigil.ThinkingPart(derefString(block.GetData()))
		part.Metadata.ProviderType = typ
		return part, true
	case "tool_use", "server_tool_use", "mcp_tool_use":
		inputJSON, _ := marshalAny(derefAny(block.GetInput()))
		part := sigil.ToolCallPart(sigil.ToolCall{
			ID:        derefString(block.GetID()),
			Name:      derefString(block.GetName()),
			InputJSON: inputJSON,
		})
		part.Metadata.ProviderType = typ
		return part, true
	case "tool_result",
		"web_search_tool_result",
		"web_fetch_tool_result",
		"code_execution_tool_result",
		"bash_code_execution_tool_result",
		"text_editor_code_execution_tool_result",
		"tool_search_tool_result",
		"mcp_tool_result":
		contentJSON, _ := marshalAny(block)
		part := sigil.ToolResultPart(sigil.ToolResult{
			ToolCallID:  derefString(block.GetToolUseID()),
			IsError:     derefBool(block.GetIsError()),
			ContentJSON: contentJSON,
		})
		part.Metadata.ProviderType = typ
		return part, true
	default:
		return sigil.Part{}, false
	}
}

func mapResponseBlock(block asdk.BetaContentBlockUnion) (sigil.Part, bool) {
	switch block.Type {
	case "text":
		text := strings.TrimSpace(block.Text)
		if text == "" {
			return sigil.Part{}, false
		}
		return sigil.TextPart(text), true
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

func mapTools(tools []asdk.BetaToolUnionParam) []sigil.ToolDefinition {
	if len(tools) == 0 {
		return nil
	}

	out := make([]sigil.ToolDefinition, 0, len(tools))
	for i := range tools {
		name := derefString(tools[i].GetName())
		if strings.TrimSpace(name) == "" {
			continue
		}

		definition := sigil.ToolDefinition{
			Name:        name,
			Description: derefString(tools[i].GetDescription()),
			Type:        derefString(tools[i].GetType()),
		}

		if schema := tools[i].GetInputSchema(); schema != nil {
			raw, err := marshalAny(*schema)
			if err == nil {
				definition.InputSchema = raw
			}
		}

		out = append(out, definition)
	}

	return out
}

func mapSystemPrompt(system []asdk.BetaTextBlockParam) string {
	if len(system) == 0 {
		return ""
	}

	parts := make([]string, 0, len(system))
	for i := range system {
		text := strings.TrimSpace(system[i].Text)
		if text == "" {
			continue
		}
		parts = append(parts, text)
	}

	return strings.Join(parts, "\n\n")
}

func mapUsage(usage asdk.BetaUsage) sigil.TokenUsage {
	cacheCreation := usage.CacheCreationInputTokens
	return sigil.TokenUsage{
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		TotalTokens:              usage.InputTokens + usage.OutputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		CacheWriteInputTokens:    cacheCreation,
		CacheCreationInputTokens: cacheCreation,
	}
}

func mapDeltaUsage(usage asdk.BetaMessageDeltaUsage) sigil.TokenUsage {
	cacheCreation := usage.CacheCreationInputTokens
	return sigil.TokenUsage{
		InputTokens:              usage.InputTokens,
		OutputTokens:             usage.OutputTokens,
		TotalTokens:              usage.InputTokens + usage.OutputTokens,
		CacheReadInputTokens:     usage.CacheReadInputTokens,
		CacheWriteInputTokens:    cacheCreation,
		CacheCreationInputTokens: cacheCreation,
	}
}

func mapRequestRole(role asdk.BetaMessageParamRole) sigil.Role {
	if role == asdk.BetaMessageParamRoleAssistant {
		return sigil.RoleAssistant
	}
	return sigil.RoleUser
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

func derefBool(value *bool) bool {
	if value == nil {
		return false
	}
	return *value
}

func derefAny(value *any) any {
	if value == nil {
		return nil
	}
	return *value
}

func marshalAny(value any) ([]byte, error) {
	if value == nil {
		return nil, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func cloneStringMap(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}

	out := make(map[string]string, len(in))
	for key, value := range in {
		out[key] = value
	}

	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}

	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}

	return out
}
