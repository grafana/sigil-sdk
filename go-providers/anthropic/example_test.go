package anthropic

import (
	asdk "github.com/anthropics/anthropic-sdk-go"
)

func ExampleFromRequestResponse() {
	req := asdk.BetaMessageNewParams{
		Model: asdk.Model("claude-sonnet-4-5"),
		Messages: []asdk.BetaMessageParam{
			{
				Role: asdk.BetaMessageParamRoleUser,
				Content: []asdk.BetaContentBlockParamUnion{
					asdk.NewBetaTextBlock("Hello"),
				},
			},
		},
	}
	resp := &asdk.BetaMessage{
		Model:      asdk.Model("claude-sonnet-4-5"),
		StopReason: asdk.BetaStopReasonEndTurn,
		Content: []asdk.BetaContentBlockUnion{
			{Type: "text", Text: "Hi!"},
		},
	}

	generation, err := FromRequestResponse(req, resp, WithThreadID("thread-1"))
	if err != nil {
		panic(err)
	}

	_ = generation.Input
	_ = generation.Output
}

func ExampleFromStream() {
	req := asdk.BetaMessageNewParams{
		Model: asdk.Model("claude-sonnet-4-5"),
		Messages: []asdk.BetaMessageParam{
			{
				Role: asdk.BetaMessageParamRoleUser,
				Content: []asdk.BetaContentBlockParamUnion{
					asdk.NewBetaTextBlock("Hello"),
				},
			},
		},
	}
	summary := StreamSummary{
		Events: []asdk.BetaRawMessageStreamEventUnion{
			{
				Type: "message_start",
				Message: asdk.BetaMessage{
					Model: asdk.Model("claude-sonnet-4-5"),
				},
			},
			{
				Type: "content_block_start",
				ContentBlock: asdk.BetaRawContentBlockStartEventContentBlockUnion{
					Type: "text",
					Text: "Hi!",
				},
			},
			{
				Type: "message_delta",
				Delta: asdk.BetaRawMessageStreamEventUnionDelta{
					StopReason: asdk.BetaStopReasonEndTurn,
				},
			},
		},
	}

	generation, err := FromStream(req, summary, WithThreadID("thread-2"))
	if err != nil {
		panic(err)
	}

	_ = generation.Input
	_ = generation.Output
}
