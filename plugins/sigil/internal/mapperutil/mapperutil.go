// Package mapperutil holds helpers shared by the per-agent mappers that turn
// captured hook fragments into Sigil generations: deterministic ID hashing,
// content-mode normalization, tool-definition building, and model→provider
// inference.
//
// Behaviour here is the common denominator across adapters. Mappers that need
// different semantics keep their own local helper and document the difference
// (e.g. copilot uses a stricter provider matcher than InferProvider).
package mapperutil

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/grafana/sigil-sdk/go/sigil"
)

// DeterministicID derives a stable, collision-resistant ID from parts. The
// parts are joined with a NUL separator (so "a","bc" and "ab","c" cannot
// collide), hashed with SHA-256, and the first 24 hex chars are appended to
// prefix as "<prefix>-<hash>". codex and copilot use it to derive generation
// IDs from (sessionID, turnID).
func DeterministicID(prefix string, parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return prefix + "-" + hex.EncodeToString(sum[:])[:24]
}

// NormalizeContentMode collapses ContentCaptureMode to the three modes mappers
// actually branch on: Default → MetadataOnly (matching envconfig resolution,
// re-applied here for callers that bypass the config layer) and
// FullWithMetadataSpans → Full (the two differ only in OTel-span exposure; the
// gRPC payload buildMessages produces is identical, so mappers can treat them
// the same when emitting content).
func NormalizeContentMode(mode sigil.ContentCaptureMode) sigil.ContentCaptureMode {
	switch mode {
	case sigil.ContentCaptureModeDefault:
		return sigil.ContentCaptureModeMetadataOnly
	case sigil.ContentCaptureModeFullWithMetadataSpans:
		return sigil.ContentCaptureModeFull
	default:
		return mode
	}
}

// SortedToolDefinitions returns deduplicated, name-sorted function tool
// definitions built from names. Empty names are skipped and nil is returned
// when no non-empty names remain. Sorting keeps the emitted Tools slice stable
// across turns and across tool subsets, which downstream consumers (tests, log
// diffing, dashboards) rely on.
func SortedToolDefinitions(names []string) []sigil.ToolDefinition {
	seen := make(map[string]struct{}, len(names))
	uniq := make([]string, 0, len(names))
	for _, name := range names {
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		uniq = append(uniq, name)
	}
	if len(uniq) == 0 {
		return nil
	}
	sort.Strings(uniq)
	out := make([]sigil.ToolDefinition, len(uniq))
	for i, name := range uniq {
		out[i] = sigil.ToolDefinition{Name: name, Type: "function"}
	}
	return out
}

// InferProvider maps a model name to a Sigil provider using loose substring
// matching for Claude/Gemini and prefix matching for the OpenAI families.
// Returns "" when nothing matches so callers can supply their own fallback.
//
// copilot intentionally uses a stricter matcher (hyphenated prefixes) and does
// not call this.
func InferProvider(model string) string {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "claude"):
		return "anthropic"
	case strings.HasPrefix(m, "gpt"),
		strings.HasPrefix(m, "o1"),
		strings.HasPrefix(m, "o3"),
		strings.HasPrefix(m, "o4"):
		return "openai"
	case strings.Contains(m, "gemini"):
		return "google"
	}
	return ""
}
