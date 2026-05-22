// Package maputil holds small generic map helpers shared across the agent
// mappers.
package maputil

import "maps"

// Clone returns a shallow copy of in, or nil when in is empty. The agent
// mappers use it to keep the GenerationStart and Generation tag/metadata maps
// independent, so a caller mutating one (e.g. adding a start-only tag) cannot
// leak into the other.
func Clone[K comparable, V any](in map[K]V) map[K]V {
	if len(in) == 0 {
		return nil
	}
	out := make(map[K]V, len(in))
	maps.Copy(out, in)
	return out
}
