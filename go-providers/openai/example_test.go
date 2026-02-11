package openai

import (
	osdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/shared"
)

func ExampleFromRequestResponse() {
	req := osdk.ChatCompletionNewParams{
		Model: shared.ChatModel("gpt-4o-mini"),
		Messages: []osdk.ChatCompletionMessageParamUnion{
			osdk.UserMessage("Hello"),
		},
	}
	resp := &osdk.ChatCompletion{
		Model: "gpt-4o-mini",
		Choices: []osdk.ChatCompletionChoice{
			{
				FinishReason: "stop",
				Message: osdk.ChatCompletionMessage{
					Content: "Hi!",
				},
			},
		},
	}

	generation, err := FromRequestResponse(req, resp, WithConversationID("conv-1"))
	if err != nil {
		panic(err)
	}

	_ = generation.Input
	_ = generation.Output
}

func ExampleFromStream() {
	req := osdk.ChatCompletionNewParams{
		Model: shared.ChatModel("gpt-4o-mini"),
		Messages: []osdk.ChatCompletionMessageParamUnion{
			osdk.UserMessage("Hello"),
		},
	}
	summary := StreamSummary{
		Chunks: []osdk.ChatCompletionChunk{
			{
				Model: "gpt-4o-mini",
				Choices: []osdk.ChatCompletionChunkChoice{
					{
						Delta: osdk.ChatCompletionChunkChoiceDelta{
							Content: "Hi!",
						},
						FinishReason: "stop",
					},
				},
			},
		},
	}

	generation, err := FromStream(req, summary, WithConversationID("conv-2"))
	if err != nil {
		panic(err)
	}

	_ = generation.Input
	_ = generation.Output
}
