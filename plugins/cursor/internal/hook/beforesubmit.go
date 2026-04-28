package hook

import (
	"log"

	"github.com/grafana/sigil-sdk/plugins/cursor/internal/fragment"
)

// BeforeSubmit captures the user prompt for the upcoming generation. Cursor
// doesn't always assign a generation_id at prompt-submit time; without one we
// cannot key the fragment, so skip silently — the turn will still be exported,
// just without the user prompt in `input`.
func BeforeSubmit(p Payload, logger *log.Logger) {
	if p.ConversationID == "" || p.GenerationID == "" {
		logger.Print("beforeSubmitPrompt: no generation_id yet — skipping")
		return
	}
	ts := p.ResolvedTimestamp()

	err := fragment.Update(p.ConversationID, p.GenerationID, logger, func(f *fragment.Fragment) bool {
		fragment.Touch(f, ts)
		if p.Prompt != "" {
			f.UserPrompt = p.Prompt
		}
		return true
	})
	if err != nil {
		logger.Printf("beforeSubmitPrompt: save: %v", err)
		return
	}
	logger.Printf("beforeSubmitPrompt: captured gen=%s promptLen=%d", p.GenerationID, len(p.Prompt))
}
