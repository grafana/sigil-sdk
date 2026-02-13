package openai

import (
	"encoding/json"
	"errors"
	"strings"

	osdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/shared"

	"github.com/grafana/sigil/sdks/go/sigil"
)

// FromRequestResponse maps an OpenAI chat completion request/response pair to sigil.Generation.
func FromRequestResponse(req osdk.ChatCompletionNewParams, resp *osdk.ChatCompletion, opts ...Option) (sigil.Generation, error) {
	if resp == nil {
		return sigil.Generation{}, errors.New("response is required")
	}

	options := applyOptions(opts)
	input, systemPrompt := mapRequestMessages(req.Messages)
	output := mapResponseMessages(resp.Choices)

	artifacts := make([]sigil.Artifact, 0, 3)
	if options.includeRequestArtifact {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindRequest, "openai.chat.request", req)
		if err != nil {
			return sigil.Generation{}, err
		}
		artifacts = append(artifacts, artifact)
	}
	if options.includeResponseArtifact {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindResponse, "openai.chat.response", resp)
		if err != nil {
			return sigil.Generation{}, err
		}
		artifacts = append(artifacts, artifact)
	}
	if options.includeToolsArtifact && len(req.Tools) > 0 {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindTools, "openai.chat.tools", req.Tools)
		if err != nil {
			return sigil.Generation{}, err
		}
		artifacts = append(artifacts, artifact)
	}

	requestModel := string(req.Model)
	responseModel := strings.TrimSpace(resp.Model)
	if responseModel == "" {
		responseModel = requestModel
	}

	generation := sigil.Generation{
		ConversationID: options.conversationID,
		AgentName:      options.agentName,
		AgentVersion:   options.agentVersion,
		Model:          sigil.ModelRef{Provider: options.providerName, Name: requestModel},
		ResponseID:     resp.ID,
		ResponseModel:  responseModel,
		SystemPrompt:   systemPrompt,
		Input:          input,
		Output:         output,
		Tools:          mapTools(req.Tools),
		Usage:          mapUsage(resp.Usage),
		StopReason:     firstFinishReason(resp.Choices),
		Tags:           cloneStringMap(options.tags),
		Metadata:       cloneAnyMap(options.metadata),
		Artifacts:      artifacts,
	}

	if err := generation.Validate(); err != nil {
		return sigil.Generation{}, err
	}

	return generation, nil
}

func mapRequestMessages(messages []osdk.ChatCompletionMessageParamUnion) ([]sigil.Message, string) {
	if len(messages) == 0 {
		return nil, ""
	}

	out := make([]sigil.Message, 0, len(messages))
	systemPrompts := make([]string, 0, 2)

	for i := range messages {
		switch {
		case messages[i].OfSystem != nil:
			systemPrompts = appendNonEmpty(systemPrompts, extractTextFromSystem(messages[i].OfSystem))
		case messages[i].OfDeveloper != nil:
			systemPrompts = appendNonEmpty(systemPrompts, extractTextFromDeveloper(messages[i].OfDeveloper))
		case messages[i].OfUser != nil:
			parts := mapUserParts(messages[i].OfUser)
			if len(parts) > 0 {
				out = append(out, sigil.Message{Role: sigil.RoleUser, Parts: parts})
			}
		case messages[i].OfAssistant != nil:
			parts := mapAssistantParamParts(messages[i].OfAssistant)
			if len(parts) > 0 {
				out = append(out, sigil.Message{Role: sigil.RoleAssistant, Parts: parts})
			}
		case messages[i].OfTool != nil:
			part := mapToolMessage(messages[i].OfTool)
			if part != nil {
				out = append(out, sigil.Message{Role: sigil.RoleTool, Parts: []sigil.Part{*part}})
			}
		case messages[i].OfFunction != nil:
			part := mapFunctionMessage(messages[i].OfFunction)
			if part != nil {
				out = append(out, sigil.Message{Role: sigil.RoleTool, Parts: []sigil.Part{*part}})
			}
		}
	}

	return out, strings.Join(systemPrompts, "\n\n")
}

func mapResponseMessages(choices []osdk.ChatCompletionChoice) []sigil.Message {
	if len(choices) == 0 {
		return nil
	}

	message := choices[0].Message
	parts := make([]sigil.Part, 0, 1+len(message.ToolCalls))

	if text := strings.TrimSpace(message.Content); text != "" {
		parts = append(parts, sigil.TextPart(text))
	}
	if refusal := strings.TrimSpace(message.Refusal); refusal != "" {
		parts = append(parts, sigil.TextPart(refusal))
	}
	for _, call := range message.ToolCalls {
		part := sigil.ToolCallPart(sigil.ToolCall{
			ID:        call.ID,
			Name:      call.Function.Name,
			InputJSON: parseJSONOrString(call.Function.Arguments),
		})
		part.Metadata.ProviderType = "tool_call"
		parts = append(parts, part)
	}

	if len(parts) == 0 {
		return nil
	}

	return []sigil.Message{
		{
			Role:  sigil.RoleAssistant,
			Parts: parts,
		},
	}
}

func mapUserParts(message *osdk.ChatCompletionUserMessageParam) []sigil.Part {
	parts := make([]sigil.Part, 0, 2)
	if message.Content.OfString.Valid() {
		if text := strings.TrimSpace(message.Content.OfString.Value); text != "" {
			parts = append(parts, sigil.TextPart(text))
		}
	}
	for _, contentPart := range message.Content.OfArrayOfContentParts {
		text := strings.TrimSpace(derefString(contentPart.GetText()))
		if text != "" {
			parts = append(parts, sigil.TextPart(text))
		}
	}
	return parts
}

func mapAssistantParamParts(message *osdk.ChatCompletionAssistantMessageParam) []sigil.Part {
	parts := make([]sigil.Part, 0, 2+len(message.ToolCalls))
	if message.Content.OfString.Valid() {
		if text := strings.TrimSpace(message.Content.OfString.Value); text != "" {
			parts = append(parts, sigil.TextPart(text))
		}
	}
	for _, contentPart := range message.Content.OfArrayOfContentParts {
		if text := strings.TrimSpace(derefString(contentPart.GetText())); text != "" {
			parts = append(parts, sigil.TextPart(text))
		}
		if refusal := strings.TrimSpace(derefString(contentPart.GetRefusal())); refusal != "" {
			parts = append(parts, sigil.TextPart(refusal))
		}
	}
	if message.Refusal.Valid() {
		if refusal := strings.TrimSpace(message.Refusal.Value); refusal != "" {
			parts = append(parts, sigil.TextPart(refusal))
		}
	}
	for _, call := range message.ToolCalls {
		function := call.GetFunction()
		if function == nil {
			continue
		}
		part := sigil.ToolCallPart(sigil.ToolCall{
			ID:        strings.TrimSpace(derefString(call.GetID())),
			Name:      function.Name,
			InputJSON: parseJSONOrString(function.Arguments),
		})
		part.Metadata.ProviderType = "tool_call"
		parts = append(parts, part)
	}
	return parts
}

func mapToolMessage(message *osdk.ChatCompletionToolMessageParam) *sigil.Part {
	content := ""
	if message.Content.OfString.Valid() {
		content = strings.TrimSpace(message.Content.OfString.Value)
	} else {
		chunks := make([]string, 0, len(message.Content.OfArrayOfContentParts))
		for _, part := range message.Content.OfArrayOfContentParts {
			if text := strings.TrimSpace(part.Text); text != "" {
				chunks = append(chunks, text)
			}
		}
		content = strings.Join(chunks, "\n")
	}
	if content == "" {
		return nil
	}

	part := sigil.ToolResultPart(sigil.ToolResult{
		ToolCallID: message.ToolCallID,
		Content:    content,
	})
	part.Metadata.ProviderType = "tool_result"
	return &part
}

//nolint:staticcheck // OpenAI API still exposes this deprecated message type in union payloads.
func mapFunctionMessage(message *osdk.ChatCompletionFunctionMessageParam) *sigil.Part {
	if !message.Content.Valid() {
		return nil
	}
	content := strings.TrimSpace(message.Content.Value)
	if content == "" {
		return nil
	}

	part := sigil.ToolResultPart(sigil.ToolResult{
		Name:        message.Name,
		Content:     content,
		ContentJSON: parseJSONOrString(content),
	})
	part.Metadata.ProviderType = "function_result"
	return &part
}

func mapTools(tools []osdk.ChatCompletionToolUnionParam) []sigil.ToolDefinition {
	if len(tools) == 0 {
		return nil
	}

	out := make([]sigil.ToolDefinition, 0, len(tools))
	for i := range tools {
		function := tools[i].GetFunction()
		if function == nil {
			continue
		}
		name := function.Name
		if strings.TrimSpace(name) == "" {
			continue
		}

		definition := sigil.ToolDefinition{
			Name: name,
		}
		if toolType := tools[i].GetType(); toolType != nil {
			definition.Type = *toolType
		}
		if function.Description.Valid() {
			definition.Description = function.Description.Value
		}
		if schema := marshalFunctionSchema(*function); len(schema) > 0 {
			definition.InputSchema = schema
		}
		out = append(out, definition)
	}

	return out
}

func marshalFunctionSchema(function shared.FunctionDefinitionParam) []byte {
	if function.Parameters == nil {
		return nil
	}
	data, err := json.Marshal(function.Parameters)
	if err != nil {
		return nil
	}
	return data
}

func mapUsage(usage osdk.CompletionUsage) sigil.TokenUsage {
	return sigil.TokenUsage{
		InputTokens:          usage.PromptTokens,
		OutputTokens:         usage.CompletionTokens,
		TotalTokens:          usage.TotalTokens,
		CacheReadInputTokens: usage.PromptTokensDetails.CachedTokens,
		ReasoningTokens:      usage.CompletionTokensDetails.ReasoningTokens,
	}
}

func firstFinishReason(choices []osdk.ChatCompletionChoice) string {
	for i := range choices {
		if choices[i].FinishReason != "" {
			return choices[i].FinishReason
		}
	}
	return ""
}

func extractTextFromSystem(message *osdk.ChatCompletionSystemMessageParam) string {
	if message.Content.OfString.Valid() {
		return strings.TrimSpace(message.Content.OfString.Value)
	}
	parts := make([]string, 0, len(message.Content.OfArrayOfContentParts))
	for _, part := range message.Content.OfArrayOfContentParts {
		if text := strings.TrimSpace(part.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func extractTextFromDeveloper(message *osdk.ChatCompletionDeveloperMessageParam) string {
	if message.Content.OfString.Valid() {
		return strings.TrimSpace(message.Content.OfString.Value)
	}
	parts := make([]string, 0, len(message.Content.OfArrayOfContentParts))
	for _, part := range message.Content.OfArrayOfContentParts {
		if text := strings.TrimSpace(part.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
}

func appendNonEmpty(values []string, value string) []string {
	value = strings.TrimSpace(value)
	if value == "" {
		return values
	}
	return append(values, value)
}

func parseJSONOrString(value string) []byte {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	data := []byte(trimmed)
	if json.Valid(data) {
		return data
	}
	quoted, err := json.Marshal(trimmed)
	if err != nil {
		return nil
	}
	return quoted
}

func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
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
