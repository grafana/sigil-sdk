package genkit

import (
	"encoding/json"
	"strings"

	"github.com/firebase/genkit/go/ai"
	"github.com/grafana/sigil-sdk/go/sigil"
)

func mapMessages(msgs []*ai.Message) ([]sigil.Message, string) {
	var systemPrompt string
	var out []sigil.Message
	for _, msg := range msgs {
		if msg.Role == ai.RoleSystem {
			systemPrompt = extractSystemText(msg)
			continue
		}
		out = append(out, mapMessage(msg))
	}
	return out, systemPrompt
}

func extractSystemText(msg *ai.Message) string {
	var parts []string
	for _, p := range msg.Content {
		if p.Kind == ai.PartText && p.Text != "" {
			parts = append(parts, p.Text)
		}
	}
	return strings.Join(parts, "\n")
}

func mapMessage(msg *ai.Message) sigil.Message {
	return sigil.Message{
		Role:  mapRole(msg.Role),
		Parts: mapParts(msg.Content),
	}
}

func mapParts(parts []*ai.Part) []sigil.Part {
	var out []sigil.Part
	for _, p := range parts {
		if mapped, ok := mapPart(p); ok {
			out = append(out, mapped)
		}
	}
	return out
}

func mapPart(p *ai.Part) (sigil.Part, bool) {
	switch p.Kind {
	case ai.PartText:
		return sigil.TextPart(p.Text), true
	case ai.PartToolRequest:
		if p.ToolRequest == nil {
			return sigil.Part{}, false
		}
		inputJSON, _ := json.Marshal(p.ToolRequest.Input)
		return sigil.ToolCallPart(sigil.ToolCall{
			ID:        p.ToolRequest.Ref,
			Name:      p.ToolRequest.Name,
			InputJSON: inputJSON,
		}), true
	case ai.PartToolResponse:
		if p.ToolResponse == nil {
			return sigil.Part{}, false
		}
		var outputJSON []byte
		content := ""
		if p.ToolResponse.Output != nil {
			outputJSON, _ = json.Marshal(p.ToolResponse.Output)
			if s, ok := p.ToolResponse.Output.(string); ok {
				content = s
			}
		} else if len(p.ToolResponse.Content) > 0 {
			outputJSON, _ = json.Marshal(p.ToolResponse.Content)
		}
		return sigil.ToolResultPart(sigil.ToolResult{
			ToolCallID:  p.ToolResponse.Ref,
			Name:        p.ToolResponse.Name,
			Content:     content,
			ContentJSON: outputJSON,
		}), true
	case ai.PartReasoning:
		return sigil.ThinkingPart(p.Text), true
	default:
		return sigil.Part{}, false
	}
}

func mapRole(role ai.Role) sigil.Role {
	switch role {
	case ai.RoleUser:
		return sigil.RoleUser
	case ai.RoleModel:
		return sigil.RoleAssistant
	case ai.RoleTool:
		return sigil.RoleTool
	default:
		return sigil.RoleUser
	}
}

func mapUsage(usage *ai.GenerationUsage) sigil.TokenUsage {
	if usage == nil {
		return sigil.TokenUsage{}
	}
	return sigil.TokenUsage{
		InputTokens:          int64(usage.InputTokens),
		OutputTokens:         int64(usage.OutputTokens),
		TotalTokens:          int64(usage.TotalTokens),
		CacheReadInputTokens: int64(usage.CachedContentTokens),
		ReasoningTokens:      int64(usage.ThoughtsTokens),
	}
}

func mapTools(tools []*ai.ToolDefinition) []sigil.ToolDefinition {
	if len(tools) == 0 {
		return nil
	}
	out := make([]sigil.ToolDefinition, 0, len(tools))
	for _, t := range tools {
		schema, _ := json.Marshal(t.InputSchema)
		out = append(out, sigil.ToolDefinition{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return out
}

func mapToolChoice(tc ai.ToolChoice) *string {
	if tc == "" {
		return nil
	}
	s := string(tc)
	return &s
}

func extractModelConfig(config any) (maxTokens *int64, temperature *float64, topP *float64) {
	switch c := config.(type) {
	case *ai.GenerationCommonConfig:
		if c == nil {
			return nil, nil, nil
		}
		return extractModelConfig(*c)
	case ai.GenerationCommonConfig:
		if c.MaxOutputTokens > 0 {
			v := int64(c.MaxOutputTokens)
			maxTokens = &v
		}
		if c.Temperature >= 0 {
			temperature = &c.Temperature
		}
		if c.TopP >= 0 {
			topP = &c.TopP
		}
	case map[string]any:
		if v, ok := c["maxOutputTokens"]; ok {
			switch n := v.(type) {
			case float64:
				if n > 0 {
					i := int64(n)
					maxTokens = &i
				}
			case int:
				if n > 0 {
					i := int64(n)
					maxTokens = &i
				}
			}
		}
		if v, ok := c["temperature"]; ok {
			if f, ok := v.(float64); ok {
				temperature = &f
			}
		}
		if v, ok := c["topP"]; ok {
			if f, ok := v.(float64); ok {
				topP = &f
			}
		}
	}
	return maxTokens, temperature, topP
}
