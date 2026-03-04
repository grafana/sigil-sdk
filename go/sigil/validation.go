package sigil

import (
	"errors"
	"fmt"
	"strings"
)

func ValidateGeneration(g Generation) error {
	if g.Mode != "" && g.Mode != GenerationModeSync && g.Mode != GenerationModeStream {
		return errors.New("generation.mode must be one of SYNC|STREAM")
	}

	if strings.TrimSpace(g.Model.Provider) == "" {
		return errors.New("generation.model.provider is required")
	}

	if strings.TrimSpace(g.Model.Name) == "" {
		return errors.New("generation.model.name is required")
	}

	for i := range g.Input {
		if err := validateMessage("generation.input", i, g.Input[i]); err != nil {
			return err
		}
	}

	for i := range g.Output {
		if err := validateMessage("generation.output", i, g.Output[i]); err != nil {
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

func ValidateEmbeddingStart(start EmbeddingStart) error {
	if strings.TrimSpace(start.Model.Provider) == "" {
		return errors.New("embedding.model.provider is required")
	}
	if strings.TrimSpace(start.Model.Name) == "" {
		return errors.New("embedding.model.name is required")
	}
	if start.Dimensions != nil && *start.Dimensions <= 0 {
		return errors.New("embedding.dimensions must be > 0")
	}
	if start.EncodingFormat != "" && strings.TrimSpace(start.EncodingFormat) == "" {
		return errors.New("embedding.encoding_format must not be blank")
	}
	return nil
}

func ValidateEmbeddingResult(result EmbeddingResult) error {
	if result.InputCount < 0 {
		return errors.New("embedding.input_count must be >= 0")
	}
	if result.InputTokens < 0 {
		return errors.New("embedding.input_tokens must be >= 0")
	}
	if result.Dimensions != nil && *result.Dimensions <= 0 {
		return errors.New("embedding.dimensions must be > 0")
	}
	return nil
}

func validateMessage(path string, index int, message Message) error {
	switch message.Role {
	case RoleUser, RoleAssistant, RoleTool:
	default:
		return fmt.Errorf("%s[%d].role must be one of user|assistant|tool", path, index)
	}

	if len(message.Parts) == 0 {
		return fmt.Errorf("%s[%d].parts must not be empty", path, index)
	}

	for i := range message.Parts {
		if err := validatePart(path, index, i, message.Role, message.Parts[i]); err != nil {
			return err
		}
	}

	return nil
}

func validatePart(path string, messageIndex, partIndex int, role Role, part Part) error {
	switch part.Kind {
	case PartKindText, PartKindThinking, PartKindToolCall, PartKindToolResult:
	default:
		return fmt.Errorf("%s[%d].parts[%d].kind is invalid", path, messageIndex, partIndex)
	}

	fieldCount := 0
	if part.Text != "" {
		fieldCount++
	}
	if part.Thinking != "" {
		fieldCount++
	}
	if part.ToolCall != nil {
		fieldCount++
	}
	if part.ToolResult != nil {
		fieldCount++
	}

	if fieldCount != 1 {
		return fmt.Errorf("%s[%d].parts[%d] must set exactly one payload field", path, messageIndex, partIndex)
	}

	switch part.Kind {
	case PartKindText:
		if part.Text == "" {
			return fmt.Errorf("%s[%d].parts[%d].text is required", path, messageIndex, partIndex)
		}
	case PartKindThinking:
		if role != RoleAssistant {
			return fmt.Errorf("%s[%d].parts[%d].thinking only allowed for assistant role", path, messageIndex, partIndex)
		}
		if part.Thinking == "" {
			return fmt.Errorf("%s[%d].parts[%d].thinking is required", path, messageIndex, partIndex)
		}
	case PartKindToolCall:
		if role != RoleAssistant {
			return fmt.Errorf("%s[%d].parts[%d].tool_call only allowed for assistant role", path, messageIndex, partIndex)
		}
		if part.ToolCall == nil || strings.TrimSpace(part.ToolCall.Name) == "" {
			return fmt.Errorf("%s[%d].parts[%d].tool_call.name is required", path, messageIndex, partIndex)
		}
	case PartKindToolResult:
		if role != RoleTool {
			return fmt.Errorf("%s[%d].parts[%d].tool_result only allowed for tool role", path, messageIndex, partIndex)
		}
		if part.ToolResult == nil {
			return fmt.Errorf("%s[%d].parts[%d].tool_result is required", path, messageIndex, partIndex)
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
