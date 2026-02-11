package sigil

import "testing"

func TestValidateGenerationRolePartCompatibility(t *testing.T) {
	base := Generation{
		Model: ModelRef{
			Provider: "anthropic",
			Name:     "claude-sonnet-4-5",
		},
		Messages: []Message{
			{
				Role:  RoleAssistant,
				Parts: []Part{TextPart("ok")},
			},
		},
	}

	t.Run("tool call only assistant", func(t *testing.T) {
		g := cloneGeneration(base)
		g.Messages = append(g.Messages, Message{
			Role: RoleUser,
			Parts: []Part{
				ToolCallPart(ToolCall{Name: "weather"}),
			},
		})

		if err := ValidateGeneration(g); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("tool result only tool", func(t *testing.T) {
		g := cloneGeneration(base)
		g.Messages = append(g.Messages, Message{
			Role: RoleAssistant,
			Parts: []Part{
				ToolResultPart(ToolResult{ToolCallID: "toolu_1", Content: "sunny"}),
			},
		})

		if err := ValidateGeneration(g); err == nil {
			t.Fatalf("expected validation error")
		}
	})

	t.Run("thinking only assistant", func(t *testing.T) {
		g := cloneGeneration(base)
		g.Messages = append(g.Messages, Message{
			Role: RoleUser,
			Parts: []Part{
				ThinkingPart("private reasoning"),
			},
		})

		if err := ValidateGeneration(g); err == nil {
			t.Fatalf("expected validation error")
		}
	})
}
