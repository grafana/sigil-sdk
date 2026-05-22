package hook

import (
	"testing"
	"time"

	"github.com/grafana/sigil-sdk/plugins/sigil/internal/sigilemit"
)

// Two tool records with different completedAt timestamps must produce
// distinct, non-overlapping windows so the UI can show the real
// CALL→TOOL→CALL→TOOL interleaving instead of stacking spans at the end.
// The window math lives in sigilemit.ToolSpanWindow; this guards cursor's
// reliance on it.
func TestToolSpanWindow_PreservesInterleaving(t *testing.T) {
	genEnd := time.Date(2026, 4, 28, 12, 0, 30, 0, time.UTC)
	dur := func(ms float64) *float64 { return &ms }

	_, firstEnd := sigilemit.ToolSpanWindow("2026-04-28T12:00:05Z", dur(1000), genEnd)
	secondStart, _ := sigilemit.ToolSpanWindow("2026-04-28T12:00:20Z", dur(1000), genEnd)
	if !firstEnd.Before(secondStart) {
		t.Errorf("first.completedAt (%s) should precede second.startedAt (%s)", firstEnd, secondStart)
	}
}
