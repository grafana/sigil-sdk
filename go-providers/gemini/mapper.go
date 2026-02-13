package gemini

import (
	"encoding/json"
	"errors"
	"strings"

	"google.golang.org/genai"

	"github.com/grafana/sigil/sdks/go/sigil"
)

const thinkingBudgetMetadataKey = "sigil.gen_ai.request.thinking.budget_tokens"

// GenerateContentRequest wraps Gemini generate-content arguments in one mapper input type.
type GenerateContentRequest struct {
	Model    string                       `json:"model"`
	Contents []*genai.Content             `json:"contents,omitempty"`
	Config   *genai.GenerateContentConfig `json:"config,omitempty"`
}

// FromRequestResponse maps a Gemini request/response pair to sigil.Generation.
func FromRequestResponse(req GenerateContentRequest, resp *genai.GenerateContentResponse, opts ...Option) (sigil.Generation, error) {
	if resp == nil {
		return sigil.Generation{}, errors.New("response is required")
	}
	if strings.TrimSpace(req.Model) == "" {
		return sigil.Generation{}, errors.New("request model is required")
	}

	options := applyOptions(opts)
	input := mapContents(req.Contents)
	output, stopReason := mapCandidates(resp.Candidates)
	maxTokens, temperature, topP, toolChoice, thinkingEnabled, thinkingBudget := mapRequestControls(req.Config)

	artifacts := make([]sigil.Artifact, 0, 3)
	if options.includeRequestArtifact {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindRequest, "gemini.generate_content.request", req)
		if err != nil {
			return sigil.Generation{}, err
		}
		artifacts = append(artifacts, artifact)
	}
	if options.includeResponseArtifact {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindResponse, "gemini.generate_content.response", resp)
		if err != nil {
			return sigil.Generation{}, err
		}
		artifacts = append(artifacts, artifact)
	}
	if options.includeToolsArtifact && hasFunctionTools(req.Config) {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindTools, "gemini.generate_content.tools", req.Config.Tools)
		if err != nil {
			return sigil.Generation{}, err
		}
		artifacts = append(artifacts, artifact)
	}

	metadata := cloneAnyMap(options.metadata)
	if resp.ModelVersion != "" {
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["model_version"] = resp.ModelVersion
	}

	generation := sigil.Generation{
		ConversationID:  options.conversationID,
		AgentName:       options.agentName,
		AgentVersion:    options.agentVersion,
		Model:           sigil.ModelRef{Provider: options.providerName, Name: req.Model},
		ResponseID:      resp.ResponseID,
		ResponseModel:   resp.ModelVersion,
		SystemPrompt:    extractSystemPrompt(req.Config),
		Input:           input,
		Output:          output,
		Tools:           mapTools(req.Config),
		MaxTokens:       maxTokens,
		Temperature:     temperature,
		TopP:            topP,
		ToolChoice:      toolChoice,
		ThinkingEnabled: thinkingEnabled,
		Usage:           mapUsage(resp.UsageMetadata),
		StopReason:      stopReason,
		Tags:            cloneStringMap(options.tags),
		Metadata:        mergeThinkingBudgetMetadata(metadata, thinkingBudget),
		Artifacts:       artifacts,
	}

	if err := generation.Validate(); err != nil {
		return sigil.Generation{}, err
	}

	return generation, nil
}

func mapContents(contents []*genai.Content) []sigil.Message {
	if len(contents) == 0 {
		return nil
	}

	out := make([]sigil.Message, 0, len(contents)+1)
	for _, content := range contents {
		if content == nil {
			continue
		}

		role := mapRole(content.Role)
		roleParts := make([]sigil.Part, 0, len(content.Parts))
		assistantParts := make([]sigil.Part, 0, 1)
		toolParts := make([]sigil.Part, 0, 1)

		for _, part := range content.Parts {
			if part == nil {
				continue
			}

			if text := strings.TrimSpace(part.Text); text != "" {
				if part.Thought && role == sigil.RoleAssistant {
					roleParts = append(roleParts, sigil.ThinkingPart(text))
				} else {
					roleParts = append(roleParts, sigil.TextPart(text))
				}
			}

			if part.FunctionCall != nil {
				call := sigil.ToolCallPart(sigil.ToolCall{
					ID:        part.FunctionCall.ID,
					Name:      part.FunctionCall.Name,
					InputJSON: marshalAny(part.FunctionCall.Args),
				})
				call.Metadata.ProviderType = "function_call"
				if role == sigil.RoleAssistant {
					roleParts = append(roleParts, call)
				} else {
					assistantParts = append(assistantParts, call)
				}
			}

			if part.FunctionResponse != nil {
				result := sigil.ToolResultPart(sigil.ToolResult{
					ToolCallID:  part.FunctionResponse.ID,
					Name:        part.FunctionResponse.Name,
					ContentJSON: marshalAny(part.FunctionResponse.Response),
				})
				result.Metadata.ProviderType = "function_response"
				toolParts = append(toolParts, result)
			}
		}

		if len(roleParts) > 0 {
			out = append(out, sigil.Message{Role: role, Parts: roleParts})
		}
		if len(assistantParts) > 0 {
			out = append(out, sigil.Message{Role: sigil.RoleAssistant, Parts: assistantParts})
		}
		if len(toolParts) > 0 {
			out = append(out, sigil.Message{Role: sigil.RoleTool, Parts: toolParts})
		}
	}

	return out
}

func mapCandidates(candidates []*genai.Candidate) ([]sigil.Message, string) {
	if len(candidates) == 0 {
		return nil, ""
	}

	out := make([]sigil.Message, 0, len(candidates))
	stopReason := ""
	for _, candidate := range candidates {
		if candidate == nil {
			continue
		}
		if stopReason == "" && candidate.FinishReason != "" {
			stopReason = string(candidate.FinishReason)
		}

		contentMessages := mapContents([]*genai.Content{candidate.Content})
		out = append(out, contentMessages...)
	}

	return out, stopReason
}

func mapTools(config *genai.GenerateContentConfig) []sigil.ToolDefinition {
	if config == nil || len(config.Tools) == 0 {
		return nil
	}

	out := make([]sigil.ToolDefinition, 0, len(config.Tools))
	for _, tool := range config.Tools {
		if tool == nil {
			continue
		}
		for _, declaration := range tool.FunctionDeclarations {
			if declaration == nil || strings.TrimSpace(declaration.Name) == "" {
				continue
			}
			definition := sigil.ToolDefinition{
				Name:        declaration.Name,
				Description: declaration.Description,
				Type:        "function",
			}
			if declaration.ParametersJsonSchema != nil {
				definition.InputSchema = marshalAny(declaration.ParametersJsonSchema)
			} else if declaration.Parameters != nil {
				definition.InputSchema = marshalAny(declaration.Parameters)
			}
			out = append(out, definition)
		}
	}

	return out
}

func mapUsage(usage *genai.GenerateContentResponseUsageMetadata) sigil.TokenUsage {
	if usage == nil {
		return sigil.TokenUsage{}
	}

	totalTokens := int64(usage.TotalTokenCount)
	if totalTokens == 0 {
		totalTokens = int64(usage.PromptTokenCount) + int64(usage.CandidatesTokenCount)
	}

	return sigil.TokenUsage{
		InputTokens:          int64(usage.PromptTokenCount),
		OutputTokens:         int64(usage.CandidatesTokenCount),
		TotalTokens:          totalTokens,
		CacheReadInputTokens: int64(usage.CachedContentTokenCount),
		ReasoningTokens:      int64(usage.ThoughtsTokenCount),
	}
}

func mapRole(role string) sigil.Role {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "model", "assistant":
		return sigil.RoleAssistant
	case "tool":
		return sigil.RoleTool
	default:
		return sigil.RoleUser
	}
}

func extractSystemPrompt(config *genai.GenerateContentConfig) string {
	if config == nil || config.SystemInstruction == nil {
		return ""
	}
	parts := make([]string, 0, len(config.SystemInstruction.Parts))
	for _, part := range config.SystemInstruction.Parts {
		if part == nil {
			continue
		}
		if text := strings.TrimSpace(part.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n\n")
}

func hasFunctionTools(config *genai.GenerateContentConfig) bool {
	if config == nil {
		return false
	}
	for _, tool := range config.Tools {
		if tool != nil && len(tool.FunctionDeclarations) > 0 {
			return true
		}
	}
	return false
}

func mapRequestControls(config *genai.GenerateContentConfig) (*int64, *float64, *float64, *string, *bool, *int64) {
	if config == nil {
		return nil, nil, nil, nil, nil, nil
	}

	var maxTokens *int64
	if config.MaxOutputTokens > 0 {
		value := int64(config.MaxOutputTokens)
		maxTokens = &value
	}

	var temperature *float64
	if config.Temperature != nil {
		value := float64(*config.Temperature)
		temperature = &value
	}

	var topP *float64
	if config.TopP != nil {
		value := float64(*config.TopP)
		topP = &value
	}

	var toolChoice *string
	if config.ToolConfig != nil && config.ToolConfig.FunctionCallingConfig != nil {
		mode := strings.ToLower(strings.TrimSpace(string(config.ToolConfig.FunctionCallingConfig.Mode)))
		if mode != "" && mode != "mode_unspecified" {
			toolChoice = &mode
		}
	}

	var thinkingEnabled *bool
	var thinkingBudget *int64
	if config.ThinkingConfig != nil {
		value := config.ThinkingConfig.IncludeThoughts
		thinkingEnabled = &value
		if config.ThinkingConfig.ThinkingBudget != nil {
			budget := int64(*config.ThinkingConfig.ThinkingBudget)
			thinkingBudget = &budget
		}
	}

	return maxTokens, temperature, topP, toolChoice, thinkingEnabled, thinkingBudget
}

func marshalAny(value any) []byte {
	if value == nil {
		return nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	return data
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

func mergeThinkingBudgetMetadata(metadata map[string]any, thinkingBudget *int64) map[string]any {
	out := cloneAnyMap(metadata)
	if thinkingBudget == nil {
		return out
	}
	if out == nil {
		out = map[string]any{}
	}
	out[thinkingBudgetMetadataKey] = *thinkingBudget
	return out
}
