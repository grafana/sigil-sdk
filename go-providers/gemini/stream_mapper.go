package gemini

import (
	"errors"
	"strings"

	"google.golang.org/genai"

	"github.com/grafana/sigil/sdks/go/sigil"
)

// StreamSummary captures Gemini streamed responses.
type StreamSummary struct {
	Responses []*genai.GenerateContentResponse
}

// FromStream maps Gemini streaming output to sigil.Generation.
func FromStream(req GenerateContentRequest, summary StreamSummary, opts ...Option) (sigil.Generation, error) {
	if strings.TrimSpace(req.Model) == "" {
		return sigil.Generation{}, errors.New("request model is required")
	}
	if len(summary.Responses) == 0 {
		return sigil.Generation{}, errors.New("stream summary has no responses")
	}

	options := applyOptions(opts)
	input := mapContents(req.Contents)
	output := make([]sigil.Message, 0, len(summary.Responses))
	stopReason := ""
	usage := sigil.TokenUsage{}

	for _, response := range summary.Responses {
		if response == nil {
			continue
		}

		candidateMessages, candidateStop := mapCandidates(response.Candidates)
		output = append(output, candidateMessages...)
		if candidateStop != "" {
			stopReason = candidateStop
		}
		if response.UsageMetadata != nil {
			usage = mapUsage(response.UsageMetadata)
		}
	}

	artifacts := make([]sigil.Artifact, 0, 3)
	if options.includeRequestArtifact {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindRequest, "gemini.generate_content.request", req)
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
	if options.includeEventsArtifact {
		artifact, err := sigil.NewJSONArtifact(sigil.ArtifactKindProviderEvent, "gemini.generate_content.stream", summary.Responses)
		if err != nil {
			return sigil.Generation{}, err
		}
		artifacts = append(artifacts, artifact)
	}

	generation := sigil.Generation{
		ThreadID:     options.threadID,
		Model:        sigil.ModelRef{Provider: options.providerName, Name: req.Model},
		SystemPrompt: extractSystemPrompt(req.Config),
		Input:        input,
		Output:       output,
		Tools:        mapTools(req.Config),
		Usage:        usage,
		StopReason:   stopReason,
		Tags:         cloneStringMap(options.tags),
		Metadata:     cloneAnyMap(options.metadata),
		Artifacts:    artifacts,
	}

	if err := generation.Validate(); err != nil {
		return sigil.Generation{}, err
	}

	return generation, nil
}
