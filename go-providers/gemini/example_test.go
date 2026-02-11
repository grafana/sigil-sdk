package gemini

import "google.golang.org/genai"

func ExampleFromRequestResponse() {
	req := GenerateContentRequest{
		Model: "gemini-2.5-pro",
		Contents: []*genai.Content{
			genai.NewContentFromText("Hello", genai.RoleUser),
		},
	}
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				FinishReason: genai.FinishReasonStop,
				Content:      genai.NewContentFromText("Hi!", genai.RoleModel),
			},
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
	req := GenerateContentRequest{
		Model: "gemini-2.5-pro",
		Contents: []*genai.Content{
			genai.NewContentFromText("Hello", genai.RoleUser),
		},
	}
	summary := StreamSummary{
		Responses: []*genai.GenerateContentResponse{
			{
				Candidates: []*genai.Candidate{
					{
						FinishReason: genai.FinishReasonStop,
						Content:      genai.NewContentFromText("Hi!", genai.RoleModel),
					},
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
