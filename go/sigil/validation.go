package sigil

import (
	"errors"
	"fmt"
	"strings"
)

func ValidateGeneration(g Generation) error {
	if strings.TrimSpace(g.Model.Provider) == "" {
		return errors.New("generation.model.provider is required")
	}

	if strings.TrimSpace(g.Model.Name) == "" {
		return errors.New("generation.model.name is required")
	}

	for i := range g.Messages {
		if err := validateMessage(i, g.Messages[i]); err != nil {
			return err
		}
	}

	for i := range g.Tools {
		if strings.TrimSpace(g.Tools[i].Name) == "" {
			return fmt.Errorf("generation.tools[%d].name is required", i)
		}
	}

	for i := range g.Artifacts {
		if err := validateArtifact(i, g.Artifacts[i]); err != nil {
			return err
		}
	}

	return nil
}

func validateMessage(index int, message Message) error {
	switch message.Role {
	case RoleUser, RoleAssistant, RoleTool:
	default:
		return fmt.Errorf("generation.messages[%d].role must be one of user|assistant|tool", index)
	}

	if len(message.Parts) == 0 {
		return fmt.Errorf("generation.messages[%d].parts must not be empty", index)
	}

	for i := range message.Parts {
		if err := validatePart(index, i, message.Role, message.Parts[i]); err != nil {
			return err
		}
	}

	return nil
}

func validatePart(messageIndex, partIndex int, role Role, part Part) error {
	switch part.Kind {
	case PartKindText, PartKindThinking, PartKindToolCall, PartKindToolResult:
	default:
		return fmt.Errorf("generation.messages[%d].parts[%d].kind is invalid", messageIndex, partIndex)
	}

	fieldCount := 0
	if strings.TrimSpace(part.Text) != "" {
		fieldCount++
	}
	if strings.TrimSpace(part.Thinking) != "" {
		fieldCount++
	}
	if part.ToolCall != nil {
		fieldCount++
	}
	if part.ToolResult != nil {
		fieldCount++
	}

	if fieldCount != 1 {
		return fmt.Errorf("generation.messages[%d].parts[%d] must set exactly one payload field", messageIndex, partIndex)
	}

	switch part.Kind {
	case PartKindText:
		if strings.TrimSpace(part.Text) == "" {
			return fmt.Errorf("generation.messages[%d].parts[%d].text is required", messageIndex, partIndex)
		}
	case PartKindThinking:
		if role != RoleAssistant {
			return fmt.Errorf("generation.messages[%d].parts[%d].thinking only allowed for assistant role", messageIndex, partIndex)
		}
		if strings.TrimSpace(part.Thinking) == "" {
			return fmt.Errorf("generation.messages[%d].parts[%d].thinking is required", messageIndex, partIndex)
		}
	case PartKindToolCall:
		if role != RoleAssistant {
			return fmt.Errorf("generation.messages[%d].parts[%d].tool_call only allowed for assistant role", messageIndex, partIndex)
		}
		if part.ToolCall == nil || strings.TrimSpace(part.ToolCall.Name) == "" {
			return fmt.Errorf("generation.messages[%d].parts[%d].tool_call.name is required", messageIndex, partIndex)
		}
	case PartKindToolResult:
		if role != RoleTool {
			return fmt.Errorf("generation.messages[%d].parts[%d].tool_result only allowed for tool role", messageIndex, partIndex)
		}
		if part.ToolResult == nil {
			return fmt.Errorf("generation.messages[%d].parts[%d].tool_result is required", messageIndex, partIndex)
		}
	}

	return nil
}

func validateArtifact(index int, artifact Artifact) error {
	switch artifact.Kind {
	case ArtifactKindRequest, ArtifactKindResponse, ArtifactKindTools, ArtifactKindProviderEvent:
	default:
		return fmt.Errorf("generation.artifacts[%d].kind is invalid", index)
	}

	if strings.TrimSpace(artifact.RecordID) == "" && len(artifact.Payload) == 0 {
		return fmt.Errorf("generation.artifacts[%d] must provide payload or record_id", index)
	}

	return nil
}
