package hook

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/grafana/sigil-sdk/go/sigil"

	"github.com/grafana/sigil-sdk/plugins/codex/internal/codexlog"
	"github.com/grafana/sigil-sdk/plugins/codex/internal/config"
	"github.com/grafana/sigil-sdk/plugins/codex/internal/fragment"
	"github.com/grafana/sigil-sdk/plugins/codex/internal/mapper"
	"github.com/grafana/sigil-sdk/plugins/codex/internal/otel"
	"github.com/grafana/sigil-sdk/plugins/codex/internal/redact"
)

const (
	otelInstrumentationName = "sigil-codex"
	stopExportTimeout       = 20 * time.Second
)

func SessionStart(p Payload, _ config.Config, logger *log.Logger) {
	if p.SessionID == "" {
		return
	}
	if err := fragment.UpdateSession(p.SessionID, logger, func(s *fragment.Session) bool {
		if p.CWD != "" {
			s.Cwd = p.CWD
		}
		if p.Model != "" {
			s.Model = p.Model
		}
		if p.Source != "" {
			s.Source = p.Source
		}
		if p.TranscriptPath != "" {
			s.TranscriptPath = p.TranscriptPath
		}
		fragment.TouchSession(s, eventTime(p))
		return true
	}); err != nil {
		logger.Printf("sessionStart: save session: %v", err)
	}
	if p.TurnID == "" {
		recordSubagentLink(p.SessionID, p.TranscriptPath, logger)
		return
	}
	recordSubagentLink(p.SessionID, p.TranscriptPath, logger)
	if err := updateCommon(p, logger, nil); err != nil {
		logger.Printf("sessionStart: update turn: %v", err)
	}
}

func UserPromptSubmit(p Payload, cfg config.Config, logger *log.Logger) {
	if p.SessionID == "" || p.TurnID == "" {
		logger.Print("userPromptSubmit: missing session_id or turn_id")
		return
	}
	if err := updateCommon(p, logger, func(f *fragment.Fragment) {
		if cfg.ContentCapture != sigil.ContentCaptureModeMetadataOnly {
			f.Prompt = p.Prompt
		}
	}); err != nil {
		logger.Printf("userPromptSubmit: %v", err)
	}
}

func PostToolUse(p Payload, cfg config.Config, logger *log.Logger) {
	if p.SessionID == "" || p.TurnID == "" {
		logger.Print("postToolUse: missing session_id or turn_id")
		return
	}
	if err := updateCommon(p, logger, func(f *fragment.Fragment) {
		resp := p.ToolResponse
		if len(resp) == 0 {
			resp = p.ToolOutput
		}
		status := normalizeStatus(p, resp)
		errMsg := errorMessageForMode(p.Error, cfg.ContentCapture)
		if cfg.ContentCapture != sigil.ContentCaptureModeFull {
			p.ToolInput = nil
			resp = nil
		}
		duration := p.ToolDurationMs
		if duration == nil {
			duration = p.DurationMs
		}
		f.Tools = append(f.Tools, fragment.ToolRecord{
			ToolName:     p.ToolName,
			ToolUseID:    p.ToolUseID,
			ToolInput:    p.ToolInput,
			ToolResponse: resp,
			Status:       status,
			ErrorMessage: errMsg,
			Cwd:          p.CWD,
			CompletedAt:  eventTime(p),
			DurationMs:   duration,
		})
	}); err != nil {
		logger.Printf("postToolUse: %v", err)
	}
}

func Stop(p Payload, cfg config.Config, logger *log.Logger) {
	if p.SessionID == "" || p.TurnID == "" {
		logger.Print("stop: missing session_id or turn_id")
		return
	}
	if err := updateCommon(p, logger, func(f *fragment.Fragment) {
		f.CompletedAt = eventTime(p)
		f.StopHookActive = p.StopHookActive
		if p.LastAssistantMessage != nil && cfg.ContentCapture != sigil.ContentCaptureModeMetadataOnly {
			f.LastAssistantMessage = *p.LastAssistantMessage
		}
	}); err != nil {
		logger.Printf("stop: update: %v", err)
		return
	}
	if !config.HasCredentials() {
		logger.Print("stop: missing SIGIL_ENDPOINT/SIGIL_AUTH_TENANT_ID/SIGIL_AUTH_TOKEN; discarding fragment")
		if err := fragment.Delete(p.SessionID, p.TurnID); err != nil {
			logger.Printf("stop: delete fragment: %v", err)
		}
		return
	}
	frag := fragment.LoadTolerant(p.SessionID, p.TurnID, logger)
	if frag == nil {
		logger.Print("stop: no fragment")
		return
	}
	subagentLink := resolveSubagentLinkForStop(p, frag, logger)
	tokenSnapshot := tokenSnapshotForStop(p, frag, logger)
	ctx, cancel := context.WithTimeout(context.Background(), stopExportTimeout)
	defer cancel()
	providers := setupOTelIfConfigured(ctx, logger)
	if providers != nil {
		defer func() {
			if err := providers.Shutdown(ctx); err != nil {
				logger.Printf("otel: shutdown: %v", err)
			}
		}()
	}
	client := buildClient(cfg, providers, logger)
	mapped := mapper.Map(mapper.Inputs{Fragment: frag, SubagentLink: subagentLink, TokenSnapshot: tokenSnapshot, ContentCapture: cfg.ContentCapture})
	logger.Printf("stop: export id=%s conversation=%s agent=%s model=%s", mapped.Generation.ID, mapped.Generation.ConversationID, mapped.Generation.AgentName, mapped.Generation.Model.Name)
	if err := emitGeneration(ctx, client, frag, mapped, cfg.ContentCapture, logger); err != nil {
		logger.Printf("stop: emit: %v", err)
		return
	}
	if err := client.Flush(ctx); err != nil {
		logger.Printf("stop: sigil flush: %v", err)
		_ = client.Shutdown(ctx)
		return
	}
	_ = client.Shutdown(ctx)
	if providers != nil {
		if err := providers.ForceFlush(); err != nil {
			logger.Printf("stop: otel flush: %v", err)
		}
	}
	if err := fragment.Delete(p.SessionID, p.TurnID); err != nil {
		logger.Printf("stop: delete fragment: %v", err)
		return
	}
	logger.Printf("stop: emitted session=%s turn=%s", p.SessionID, p.TurnID)
}

func tokenSnapshotForStop(p Payload, frag *fragment.Fragment, logger *log.Logger) *codexlog.TokenSnapshot {
	path := firstNonEmpty(frag.TranscriptPath, p.TranscriptPath)
	if path == "" || p.TurnID == "" {
		return nil
	}
	snapshot, ok, err := codexlog.ReadTokenUsageForTurn(path, p.TurnID)
	if err != nil {
		logger.Printf("token usage: read %s: %v", path, err)
		return nil
	}
	if !ok {
		logger.Printf("token usage: no attributable snapshot for turn=%s", p.TurnID)
		return nil
	}
	return &snapshot
}

func recordSubagentLink(sessionID, transcriptPath string, logger *log.Logger) *fragment.SubagentLink {
	if sessionID == "" || transcriptPath == "" {
		return nil
	}
	meta, ok, err := codexlog.ReadSessionMeta(transcriptPath)
	if err != nil {
		logger.Printf("subagent: read session meta: %v", err)
		return nil
	}
	if !ok || meta.ThreadSource != "subagent" || meta.ParentSessionID == "" {
		return nil
	}
	childSessionID := sessionID
	if childSessionID == "" {
		childSessionID = meta.SessionID
	}
	if childSessionID == "" {
		return nil
	}
	if err := fragment.UpdateSubagentLink(childSessionID, logger, func(link *fragment.SubagentLink) bool {
		link.ParentSessionID = meta.ParentSessionID
		if meta.AgentRole != "" {
			link.AgentRole = meta.AgentRole
		}
		if meta.AgentNickname != "" {
			link.AgentNickname = meta.AgentNickname
		}
		if meta.AgentDepth != 0 {
			link.AgentDepth = meta.AgentDepth
		}
		link.Source = "transcript.session_meta"
		return true
	}); err != nil {
		logger.Printf("subagent: save link: %v", err)
		return nil
	}
	return fragment.LoadSubagentLinkTolerant(childSessionID, logger)
}

func resolveSubagentLinkForStop(p Payload, frag *fragment.Fragment, logger *log.Logger) *fragment.SubagentLink {
	link := fragment.LoadSubagentLinkTolerant(p.SessionID, logger)
	if link == nil && frag.TranscriptPath != "" {
		link = recordSubagentLink(p.SessionID, frag.TranscriptPath, logger)
	}
	if link == nil || link.ParentSessionID == "" || link.ParentGenerationID != "" {
		return link
	}

	parentSession := fragment.LoadSessionTolerant(link.ParentSessionID, logger)
	if parentSession == nil || parentSession.TranscriptPath == "" {
		return link
	}

	spawn, ok, err := codexlog.ResolveSpawnLink(parentSession.TranscriptPath, p.SessionID, mapper.GenerationID)
	if err != nil {
		logger.Printf("subagent: resolve spawn link: %v", err)
		return link
	}
	if !ok {
		return link
	}
	parentSessionID := firstNonEmpty(spawn.ParentSessionID, link.ParentSessionID)
	parentGenerationID := spawn.ParentGenerationID
	if parentGenerationID == "" && parentSessionID != "" && spawn.ParentTurnID != "" {
		parentGenerationID = mapper.GenerationID(parentSessionID, spawn.ParentTurnID)
	}
	if err := fragment.UpdateSubagentLink(p.SessionID, logger, func(link *fragment.SubagentLink) bool {
		link.ParentSessionID = parentSessionID
		link.ParentTurnID = spawn.ParentTurnID
		link.ParentGenerationID = parentGenerationID
		link.SpawnCallID = spawn.SpawnCallID
		if spawn.AgentNickname != "" {
			link.AgentNickname = spawn.AgentNickname
		}
		link.LastResolvedAt = eventTime(p)
		return true
	}); err != nil {
		logger.Printf("subagent: update resolved link: %v", err)
		return link
	}
	return fragment.LoadSubagentLinkTolerant(p.SessionID, logger)
}

func updateCommon(p Payload, logger *log.Logger, mutate func(f *fragment.Fragment)) error {
	return fragment.Update(p.SessionID, p.TurnID, logger, func(f *fragment.Fragment) bool {
		applySessionDefaults(f, fragment.LoadSessionTolerant(p.SessionID, logger))
		if p.CWD != "" {
			f.Cwd = p.CWD
		}
		if p.Model != "" {
			f.Model = p.Model
		}
		if p.Source != "" {
			f.Source = p.Source
		}
		if p.TranscriptPath != "" {
			f.TranscriptPath = p.TranscriptPath
		}
		fragment.Touch(f, eventTime(p))
		if mutate != nil {
			mutate(f)
		}
		return true
	})
}

func applySessionDefaults(f *fragment.Fragment, s *fragment.Session) {
	if s == nil {
		return
	}
	if f.Cwd == "" {
		f.Cwd = s.Cwd
	}
	if f.Model == "" {
		f.Model = s.Model
	}
	if f.Source == "" {
		f.Source = s.Source
	}
	if f.TranscriptPath == "" {
		f.TranscriptPath = s.TranscriptPath
	}
}

func setupOTelIfConfigured(ctx context.Context, logger *log.Logger) *otel.Providers {
	if otel.EndpointFromEnv() == "" {
		return nil
	}
	providers, err := otel.Setup(ctx)
	if err != nil {
		logger.Printf("otel: setup: %v", err)
		return nil
	}
	return providers
}

func buildClient(cfg config.Config, providers *otel.Providers, logger *log.Logger) *sigil.Client {
	c := sigil.Config{
		ContentCapture: cfg.ContentCapture,
		Logger:         logger,
		GenerationExport: sigil.GenerationExportConfig{
			Protocol:       sigil.GenerationExportProtocolHTTP,
			Endpoint:       strings.TrimRight(os.Getenv("SIGIL_ENDPOINT"), "/") + "/api/v1/generations:export",
			BatchSize:      100,
			FlushInterval:  100 * time.Millisecond,
			QueueSize:      16,
			MaxRetries:     1,
			InitialBackoff: 50 * time.Millisecond,
			MaxBackoff:     100 * time.Millisecond,
			Auth: sigil.AuthConfig{
				Mode:          sigil.ExportAuthModeBasic,
				BasicUser:     os.Getenv("SIGIL_AUTH_TENANT_ID"),
				BasicPassword: os.Getenv("SIGIL_AUTH_TOKEN"),
				TenantID:      os.Getenv("SIGIL_AUTH_TENANT_ID"),
			},
		},
	}
	if providers != nil {
		c.Tracer = providers.Tracer(otelInstrumentationName)
		c.Meter = providers.Meter(otelInstrumentationName)
	}
	return sigil.NewClient(c)
}

func emitGeneration(ctx context.Context, client *sigil.Client, frag *fragment.Fragment, mapped mapper.Mapped, mode sigil.ContentCaptureMode, logger *log.Logger) error {
	genCtx, rec := client.StartGeneration(ctx, mapped.Start)
	rec.SetResult(mapped.Generation, nil)
	emitToolSpans(genCtx, client, frag, mapped.Generation, mode, logger)
	rec.End()
	if err := rec.Err(); err != nil {
		return fmt.Errorf("recorder: %w", err)
	}
	return nil
}

func emitToolSpans(ctx context.Context, client *sigil.Client, frag *fragment.Fragment, gen sigil.Generation, mode sigil.ContentCaptureMode, logger *log.Logger) {
	var red *redact.Redactor
	if mode == sigil.ContentCaptureModeFull {
		red = redact.New()
	}
	for i := range frag.Tools {
		t := &frag.Tools[i]
		if t.ToolName == "" {
			continue
		}
		startedAt, completedAt := toolSpanWindow(*t, gen.CompletedAt)
		_, rec := client.StartToolExecution(ctx, sigil.ToolExecutionStart{
			ToolName:        t.ToolName,
			ToolCallID:      t.ToolUseID,
			ToolType:        "function",
			ConversationID:  gen.ConversationID,
			AgentName:       gen.AgentName,
			AgentVersion:    gen.AgentVersion,
			RequestModel:    gen.Model.Name,
			RequestProvider: gen.Model.Provider,
			StartedAt:       startedAt,
		})
		end := sigil.ToolExecutionEnd{CompletedAt: completedAt}
		if len(t.ToolInput) > 0 && red != nil {
			end.Arguments = redactSpanContent(red, t.ToolInput)
		}
		if len(t.ToolResponse) > 0 && red != nil {
			end.Result = redactSpanContent(red, t.ToolResponse)
		}
		if t.Status == "error" {
			rec.SetExecError(toolErrorOr(t.ErrorMessage))
		}
		rec.SetResult(end)
		rec.End()
		if err := rec.Err(); err != nil {
			logger.Printf("tool span: %v", err)
		}
	}
}

func redactSpanContent(red *redact.Redactor, raw json.RawMessage) string {
	if red == nil || len(raw) == 0 {
		return ""
	}
	return red.RedactJSONForText(raw)
}

func toolSpanWindow(t fragment.ToolRecord, genCompletedAt time.Time) (time.Time, time.Time) {
	completedAt := parseTimestamp(t.CompletedAt, genCompletedAt)
	startedAt := completedAt
	if t.DurationMs != nil && !completedAt.IsZero() {
		startedAt = completedAt.Add(-time.Duration(*t.DurationMs) * time.Millisecond)
	}
	return startedAt, completedAt
}

func parseTimestamp(s string, def time.Time) time.Time {
	if s == "" {
		return def
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return def
}

func normalizeStatus(p Payload, response json.RawMessage) string {
	if status := normalizeStatusString(p.Status); status != "" {
		return status
	}
	if len(p.Error) > 0 && string(p.Error) != "null" {
		return "error"
	}
	if status := statusFromToolResponse(response); status != "" {
		return status
	}
	return ""
}

func normalizeStatusString(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "error", "failed", "failure":
		return "error"
	case "completed", "complete", "success", "succeeded", "ok":
		return "completed"
	default:
		return ""
	}
}

func statusFromToolResponse(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	return statusFromValue(v)
}

func statusFromValue(v any) string {
	switch x := v.(type) {
	case map[string]any:
		for _, key := range []string{"status", "state"} {
			if status := normalizeStatusString(stringField(x, key)); status != "" {
				return status
			}
		}
		for _, key := range []string{"is_error", "isError"} {
			if b, ok := boolField(x, key); ok {
				if b {
					return "error"
				}
				return "completed"
			}
		}
		if b, ok := boolField(x, "success"); ok {
			if b {
				return "completed"
			}
			return "error"
		}
		for _, key := range []string{"exit_code", "exitCode"} {
			if code, ok := numberField(x, key); ok {
				if code == 0 {
					return "completed"
				}
				return "error"
			}
		}
		if errValue, ok := x["error"]; ok && errValue != nil {
			if emptyJSONValue(errValue) || statusFromValue(errValue) == "completed" {
				return ""
			}
			return "error"
		}
		if metadata, ok := x["metadata"]; ok {
			if status := statusFromValue(metadata); status != "" {
				return status
			}
		}
	case string:
		return normalizeStatusString(x)
	}
	return ""
}

func emptyJSONValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []any:
		return len(x) == 0
	case map[string]any:
		return len(x) == 0
	default:
		return false
	}
}

func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

func boolField(m map[string]any, key string) (bool, bool) {
	v, ok := m[key]
	if !ok {
		return false, false
	}
	b, ok := v.(bool)
	return b, ok
}

func numberField(m map[string]any, key string) (float64, bool) {
	v, ok := m[key]
	if !ok {
		return 0, false
	}
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	default:
		return 0, false
	}
}

func errorMessage(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var obj struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil && obj.Message != "" {
		return obj.Message
	}
	return string(raw)
}

func errorMessageForMode(raw json.RawMessage, mode sigil.ContentCaptureMode) string {
	if mode != sigil.ContentCaptureModeFull {
		return ""
	}
	return redact.New().Redact(errorMessage(raw))
}

func eventTime(p Payload) string {
	if p.Timestamp != "" {
		return p.Timestamp
	}
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func toolErrorOr(msg string) error {
	if msg == "" {
		return errors.New("tool returned error")
	}
	return errors.New(msg)
}
