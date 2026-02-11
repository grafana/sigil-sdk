package gemini

import (
	"testing"

	"google.golang.org/genai"

	"github.com/grafana/sigil/sdks/go/sigil"
)

func TestFromRequestResponse(t *testing.T) {
	req := GenerateContentRequest{
		Model: "gemini-2.5-pro",
		Contents: []*genai.Content{
			genai.NewContentFromText("What is the weather in Paris?", genai.RoleUser),
			genai.NewContentFromParts([]*genai.Part{
				genai.NewPartFromFunctionResponse("weather", map[string]any{
					"temp_c": 18,
				}),
			}, genai.RoleUser),
		},
		Config: &genai.GenerateContentConfig{
			SystemInstruction: genai.NewContentFromText("Be concise.", genai.RoleUser),
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{
							Name:        "weather",
							Description: "Get weather",
							ParametersJsonSchema: map[string]any{
								"type": "object",
								"properties": map[string]any{
									"city": map[string]any{"type": "string"},
								},
								"required": []string{"city"},
							},
						},
					},
				},
			},
		},
	}

	resp := &genai.GenerateContentResponse{
		ModelVersion: "gemini-2.5-pro-001",
		Candidates: []*genai.Candidate{
			{
				FinishReason: genai.FinishReasonStop,
				Content: genai.NewContentFromParts([]*genai.Part{
					{
						FunctionCall: &genai.FunctionCall{
							ID:   "call_weather",
							Name: "weather",
							Args: map[string]any{"city": "Paris"},
						},
					},
					genai.NewPartFromText("It is 18C and sunny."),
				}, genai.RoleModel),
			},
		},
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:        120,
			CandidatesTokenCount:    40,
			TotalTokenCount:         170,
			CachedContentTokenCount: 12,
			ThoughtsTokenCount:      10,
		},
	}

	generation, err := FromRequestResponse(req, resp,
		WithThreadID("thread-9b2f"),
		WithTag("tenant", "t-123"),
	)
	if err != nil {
		t.Fatalf("from request/response: %v", err)
	}

	if generation.Model.Provider != "gemini" {
		t.Fatalf("expected provider gemini, got %q", generation.Model.Provider)
	}
	if generation.Model.Name != "gemini-2.5-pro" {
		t.Fatalf("expected model gemini-2.5-pro, got %q", generation.Model.Name)
	}
	if generation.ThreadID != "thread-9b2f" {
		t.Fatalf("expected thread-9b2f, got %q", generation.ThreadID)
	}
	if generation.SystemPrompt != "Be concise." {
		t.Fatalf("unexpected system prompt: %q", generation.SystemPrompt)
	}
	if generation.StopReason != "STOP" {
		t.Fatalf("expected stop reason STOP, got %q", generation.StopReason)
	}
	if generation.Usage.TotalTokens != 170 {
		t.Fatalf("expected total tokens 170, got %d", generation.Usage.TotalTokens)
	}
	if generation.Usage.CacheReadInputTokens != 12 {
		t.Fatalf("expected cache read tokens 12, got %d", generation.Usage.CacheReadInputTokens)
	}
	if generation.Usage.ReasoningTokens != 10 {
		t.Fatalf("expected reasoning tokens 10, got %d", generation.Usage.ReasoningTokens)
	}
	if generation.Tags["tenant"] != "t-123" {
		t.Fatalf("expected tenant tag")
	}
	if len(generation.Artifacts) != 3 {
		t.Fatalf("expected 3 artifacts, got %d", len(generation.Artifacts))
	}

	hasToolRole := false
	for _, message := range generation.Messages {
		if message.Role == sigil.RoleTool {
			hasToolRole = true
		}
	}
	if !hasToolRole {
		t.Fatalf("expected tool role message from function response input")
	}
}

func TestFromStream(t *testing.T) {
	req := GenerateContentRequest{
		Model: "gemini-2.5-pro",
		Contents: []*genai.Content{
			genai.NewContentFromText("What is the weather in Paris?", genai.RoleUser),
		},
		Config: &genai.GenerateContentConfig{
			Tools: []*genai.Tool{
				{
					FunctionDeclarations: []*genai.FunctionDeclaration{
						{Name: "weather"},
					},
				},
			},
		},
	}

	summary := StreamSummary{
		Responses: []*genai.GenerateContentResponse{
			{
				Candidates: []*genai.Candidate{
					{
						Content: genai.NewContentFromParts([]*genai.Part{
							{
								FunctionCall: &genai.FunctionCall{
									ID:   "call_weather",
									Name: "weather",
									Args: map[string]any{"city": "Paris"},
								},
							},
						}, genai.RoleModel),
					},
				},
			},
			{
				Candidates: []*genai.Candidate{
					{
						FinishReason: genai.FinishReasonStop,
						Content:      genai.NewContentFromText("It is 18C and sunny.", genai.RoleModel),
					},
				},
				UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
					PromptTokenCount:     20,
					CandidatesTokenCount: 6,
					TotalTokenCount:      26,
				},
			},
		},
	}

	generation, err := FromStream(req, summary, WithThreadID("thread-stream"))
	if err != nil {
		t.Fatalf("from stream: %v", err)
	}

	if generation.ThreadID != "thread-stream" {
		t.Fatalf("expected thread-stream, got %q", generation.ThreadID)
	}
	if generation.StopReason != "STOP" {
		t.Fatalf("expected stop reason STOP, got %q", generation.StopReason)
	}
	if generation.Usage.TotalTokens != 26 {
		t.Fatalf("expected total tokens 26, got %d", generation.Usage.TotalTokens)
	}
	if len(generation.Artifacts) != 3 {
		t.Fatalf("expected 3 artifacts, got %d", len(generation.Artifacts))
	}
}
