package genkit

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/firebase/genkit/go/ai"
	"github.com/grafana/sigil-sdk/go/sigil"
)

const (
	frameworkName     = "genkit"
	frameworkSource   = "middleware"
	frameworkLanguage = "go"
)

func createMiddleware(client *sigil.Client, modelName string, opts Options) ai.ModelMiddleware {
	ref := parseModelRef(modelName)

	tags := make(map[string]string, len(opts.ExtraTags)+3)
	for k, v := range opts.ExtraTags {
		tags[k] = v
	}
	tags["sigil.framework.name"] = frameworkName
	tags["sigil.framework.source"] = frameworkSource
	tags["sigil.framework.language"] = frameworkLanguage

	metadata := make(map[string]any, len(opts.ExtraMetadata))
	for k, v := range opts.ExtraMetadata {
		metadata[k] = v
	}

	return func(next ai.ModelFunc) ai.ModelFunc {
		return func(ctx context.Context, req *ai.ModelRequest, cb ai.ModelStreamCallback) (*ai.ModelResponse, error) {
			input, systemPrompt := mapMessages(req.Messages)
			maxTokens, temperature, topP := extractModelConfig(req.Config)

			start := sigil.GenerationStart{
				ConversationID: strings.TrimSpace(opts.ConversationID),
				AgentName:      strings.TrimSpace(opts.AgentName),
				AgentVersion:   strings.TrimSpace(opts.AgentVersion),
				Model:          ref,
				SystemPrompt:   systemPrompt,
				Tools:          mapTools(req.Tools),
				MaxTokens:      maxTokens,
				Temperature:    temperature,
				TopP:           topP,
				ToolChoice:     mapToolChoice(req.ToolChoice),
				Tags:           tags,
				Metadata:       metadata,
				ContentCapture: opts.ContentCapture,
			}

			streaming := cb != nil
			var rec *sigil.GenerationRecorder
			if streaming {
				ctx, rec = client.StartStreamingGeneration(ctx, start)
			} else {
				ctx, rec = client.StartGeneration(ctx, start)
			}
			defer rec.End()

			var wrappedCb ai.ModelStreamCallback
			if streaming {
				var once sync.Once
				wrappedCb = func(cbCtx context.Context, chunk *ai.ModelResponseChunk) error {
					if chunk != nil && hasContent(chunk) {
						once.Do(func() {
							rec.SetFirstTokenAt(time.Now().UTC())
						})
					}
					return cb(cbCtx, chunk)
				}
			}

			resp, err := next(ctx, req, wrappedCb)
			if err != nil {
				rec.SetResult(sigil.Generation{Input: input}, nil)
				rec.SetCallError(err)
				return nil, err
			}
			if resp == nil {
				resp = &ai.ModelResponse{}
			}

			gen := sigil.Generation{
				Input: input,
				Usage: mapUsage(resp.Usage).Normalize(),
			}
			if resp.FinishReason != "" {
				gen.StopReason = string(resp.FinishReason)
			}
			if resp.Message != nil {
				msg := mapMessage(resp.Message)
				if resp.Message.Role == "" {
					msg.Role = sigil.RoleAssistant
				}
				gen.Output = []sigil.Message{msg}
			}

			rec.SetResult(gen, nil)
			return resp, nil
		}
	}
}

func hasContent(chunk *ai.ModelResponseChunk) bool {
	for _, p := range chunk.Content {
		if p.Kind == ai.PartText && p.Text != "" {
			return true
		}
		if p.Kind == ai.PartToolRequest || p.Kind == ai.PartToolResponse {
			return true
		}
		if p.Kind == ai.PartReasoning && p.Text != "" {
			return true
		}
	}
	return false
}
