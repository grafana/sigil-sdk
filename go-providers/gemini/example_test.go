package gemini

import "google.golang.org/genai"

func ExampleFromRequestResponse() {
	model := "gemini-2.5-pro"
	contents := []*genai.Content{
		genai.NewContentFromText("Hello", genai.RoleUser),
	}
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{
			{
				FinishReason: genai.FinishReasonStop,
				Content:      genai.NewContentFromText("Hi!", genai.RoleModel),
			},
		},
	}

	generation, err := FromRequestResponse(model, contents, nil, resp,
		WithConversationID("conv-1"),
		WithAgentName("assistant-gemini"),
		WithAgentVersion("1.0.0"),
	)
	if err != nil {
		panic(err)
	}

	_ = generation.Input
	_ = generation.Output
}

func ExampleFromStream() {
	model := "gemini-2.5-pro"
	contents := []*genai.Content{
		genai.NewContentFromText("Hello", genai.RoleUser),
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

	generation, err := FromStream(model, contents, nil, summary,
		WithConversationID("conv-2"),
		WithAgentName("assistant-gemini"),
		WithAgentVersion("1.0.0"),
	)
	if err != nil {
		panic(err)
	}

	_ = generation.Input
	_ = generation.Output
}
