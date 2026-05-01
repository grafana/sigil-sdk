package hook

import (
	"log"

	"github.com/grafana/sigil-sdk/plugins/cursor/internal/fragment"
)

// AfterAgentResponse appends an assistant text segment + token counts to the
// fragment. Cursor may emit multiple afterAgentResponse events per generation
// (e.g. one per streamed chunk); each becomes its own segment.
func AfterAgentResponse(p Payload, logger *log.Logger) {
	if p.ConversationID == "" || p.GenerationID == "" {
		logger.Print("afterAgentResponse: missing conversation_id or generation_id")
		return
	}
	ts := p.ResolvedTimestamp()

	err := fragment.Update(p.ConversationID, p.GenerationID, logger, func(f *fragment.Fragment) bool {
		fragment.Touch(f, ts)
		if p.Model != "" {
			f.Model = p.Model
		}
		if p.Provider != "" {
			f.Provider = p.Provider
		}
		if p.Text != "" {
			f.Assistant = append(f.Assistant, fragment.AssistantSegment{Text: p.Text, Timestamp: ts})
		}
		if p.InputTokens != nil || p.OutputTokens != nil ||
			p.CacheReadTokens != nil || p.CacheWriteTokens != nil {
			if f.TokenUsage == nil {
				f.TokenUsage = &fragment.TokenCounts{}
			}
			if p.InputTokens != nil {
				f.TokenUsage.InputTokens = p.InputTokens
			}
			if p.OutputTokens != nil {
				f.TokenUsage.OutputTokens = p.OutputTokens
			}
			if p.CacheReadTokens != nil {
				f.TokenUsage.CacheReadTokens = p.CacheReadTokens
			}
			if p.CacheWriteTokens != nil {
				f.TokenUsage.CacheWriteTokens = p.CacheWriteTokens
			}
		}
		return true
	})
	if err != nil {
		logger.Printf("afterAgentResponse: save: %v", err)
		return
	}
	logger.Printf("afterAgentResponse: appended gen=%s textLen=%d", p.GenerationID, len(p.Text))
}
