// Minimal AI Observability getting-started example — Go + OpenAI.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/grafana/sigil-sdk/go/sigil"
	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/shared"
)

func main() {
	ctx := context.Background()
	model := "gpt-4.1-mini"

	cfg := sigil.DefaultConfig()
	cfg.GenerationExport.Protocol = sigil.GenerationExportProtocolHTTP
	cfg.GenerationExport.Endpoint = os.Getenv("SIGIL_ENDPOINT")
	cfg.GenerationExport.Auth = sigil.AuthConfig{
		Mode:          sigil.ExportAuthModeBasic,
		TenantID:      os.Getenv("GRAFANA_INSTANCE_ID"),
		BasicPassword: os.Getenv("GRAFANA_CLOUD_TOKEN"),
	}
	sigilClient := sigil.NewClient(cfg)
	defer func() { _ = sigilClient.Shutdown(ctx) }()

	openaiClient := openai.NewClient(option.WithAPIKey(os.Getenv("OPENAI_API_KEY")))

	prompt := "Explain what LLM observability is in two sentences."

	completion, err := openaiClient.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model: shared.ChatModel(model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage("You are a helpful assistant."),
			openai.UserMessage(prompt),
		},
	})
	if err != nil {
		log.Fatalf("OpenAI error: %v", err)
	}

	responseText := completion.Choices[0].Message.Content
	fmt.Printf("Response: %s\n\n", responseText)

	ctx, rec := sigilClient.StartGeneration(ctx, sigil.GenerationStart{
		ConversationID: "getting-started-go",
		AgentName:      "getting-started",
		AgentVersion:   "1.0.0",
		Model:          sigil.ModelRef{Provider: "openai", Name: model},
	})
	defer rec.End()
	_ = ctx

	rec.SetResult(sigil.Generation{
		Input:         []sigil.Message{sigil.UserTextMessage(prompt)},
		Output:        []sigil.Message{sigil.AssistantTextMessage(responseText)},
		ResponseID:    completion.ID,
		ResponseModel: completion.Model,
		StopReason:    string(completion.Choices[0].FinishReason),
		Usage: sigil.TokenUsage{
			InputTokens:  completion.Usage.PromptTokens,
			OutputTokens: completion.Usage.CompletionTokens,
		},
	}, nil)

	fmt.Println("Done — check the AI Observability plugin in your Grafana Cloud stack.")
}
