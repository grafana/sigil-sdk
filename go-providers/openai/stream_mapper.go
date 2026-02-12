package openai

import (
	"errors"
	"strings"

	osdk "github.com/openai/openai-go"

	"github.com/grafana/sigil/sdks/go/sigil"
)

// StreamSummary captures OpenAI stream chunks and an optional final response.
type StreamSummary struct {
	Chunks        []osdk.ChatCompletionChunk
	FinalResponse *osdk.ChatCompletion
}

type streamToolCall struct {
	id        string
	name      string
	arguments strings.Builder
}

// FromStream maps OpenAI streaming output to sigil.Generation.
func FromStream(req osdk.ChatCompletionNewParams, summary StreamSummary, opts ...Option) (sigil.Generation, error) {
	if summary.FinalResponse != nil {
		generation, err := FromRequestResponse(req, summary.FinalResponse, opts...)
		if err != nil {
			return sigil.Generation{}, err
		}
		return appendStreamEventsArtifact(generation, summary.Chunks, opts)
	}

	if len(summary.Chunks) == 0 {
		return sigil.Generation{}, errors.New("stream summary has no chunks and no final response")
	}

	options := applyOptions(opts)
	input, systemPrompt := mapRequestMessages(req.Messages)
	output := make([]sigil.Message, 0, 1)

	modelName := string(req.Model)
	responseID := ""
	usage := sigil.TokenUsage{}
	stopReason := ""
	var text strings.Builder

	toolCalls := map[int64]*streamToolCall{}
	order := make([]int64, 0, 2)

	for i := range summary.Chunks {
		chunk := summary.Chunks[i]
		if chunk.ID != "" {
			responseID = chunk.ID
		}
		if chunk.Model != "" {
			modelName = chunk.Model
		}
		if chunk.Usage.TotalTokens > 0 || chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			usage = mapUsage(chunk.Usage)
		}

		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				stopReason = choice.FinishReason
			}

			delta := choice.Delta
			if delta.Content != "" {
				text.WriteString(delta.Content)
			}

			for _, toolCall := range delta.ToolCalls {
				call, ok := toolCalls[toolCall.Index]
				if !ok {
					call = &streamToolCall{}
					toolCalls[toolCall.Index] = call
					order = append(order, toolCall.Index)
				}
				if toolCall.ID != "" {
					call.id = toolCall.ID
				}
				if toolCall.Function.Name != "" {
					call.name = toolCall.Function.Name
				}
				if toolCall.Function.Arguments != "" {
					call.arguments.WriteString(toolCall.Function.Arguments)
				}
			}
		}
	}

	assistantParts := make([]sigil.Part, 0, 1+len(order))
	if generated := strings.TrimSpace(text.String()); generated != "" {
		assistantParts = append(assistantParts, sigil.TextPart(generated))
	}
	for _, index := range order {
		call := toolCalls[index]
		if call == nil || strings.TrimSpace(call.name) == "" {
			continue
		}
		part := sigil.ToolCallPart(sigil.ToolCall{
			ID:        call.id,
			Name:      call.name,
			InputJSON: parseJSONOrString(call.arguments.String()),
		})
		part.Metadata.ProviderType = "tool_call"
		assistantParts = append(assistantParts, part)
	}
	if len(assistantParts) > 0 {
		output = append(output, sigil.Message{
			Role:  sigil.RoleAssistant,
			Parts: assistantParts,
		})
	}

	artifacts := make([]sigil.Artifact, 0, 3)
	if options.includeRequestArtifact {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindRequest, "openai.chat.request", req)
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
	if options.includeEventsArtifact {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindProviderEvent, "openai.chat.stream_chunks", summary.Chunks)
		if err != nil {
			return sigil.Generation{}, err
		}
		artifacts = append(artifacts, artifact)
	}

	generation := sigil.Generation{
		ConversationID: options.conversationID,
		AgentName:      options.agentName,
		AgentVersion:   options.agentVersion,
		Model:          sigil.ModelRef{Provider: options.providerName, Name: string(req.Model)},
		ResponseID:     responseID,
		ResponseModel:  modelName,
		SystemPrompt:   systemPrompt,
		Input:          input,
		Output:         output,
		Tools:          mapTools(req.Tools),
		Usage:          usage,
		StopReason:     stopReason,
		Tags:           cloneStringMap(options.tags),
		Metadata:       cloneAnyMap(options.metadata),
		Artifacts:      artifacts,
	}

	if err := generation.Validate(); err != nil {
		return sigil.Generation{}, err
	}

	return generation, nil
}

func appendStreamEventsArtifact(generation sigil.Generation, chunks []osdk.ChatCompletionChunk, opts []Option) (sigil.Generation, error) {
	if len(chunks) == 0 {
		return generation, nil
	}

	options := applyOptions(opts)
	if !options.includeEventsArtifact {
		return generation, nil
	}

	artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindProviderEvent, "openai.chat.stream_chunks", chunks)
	if err != nil {
		return sigil.Generation{}, err
	}
	generation.Artifacts = append(generation.Artifacts, artifact)
	return generation, nil
}
