// Package sigil provides manual recording helpers for LLM generations.
//
// The primary API follows an OTel-like pattern:
//
//	ctx, rec := client.StartGeneration(ctx, start)
//	defer rec.End()
//	resp, err := provider.Call(ctx, req)
//	if err != nil { rec.SetCallError(err); return err }
//	rec.SetResult(mapper.FromRequestResponse(req, resp))
//
// For streaming calls, use:
//
//	ctx, rec := client.StartStreamingGeneration(ctx, start)
//	defer rec.End()
//	// process stream...
//	rec.SetResult(mapper.FromStream(req, summary))
//
// Tool calls can be wrapped with Client.StartToolExecution + ToolExecutionRecorder.End.
//
// Start returns a context for your provider call. End is a no-arg, no-return
// finalizer safe for defer. It is idempotent and nil-safe.
//
// # Content Capture
//
// Config.ContentCapture sets the default ContentCaptureMode (Full or MetadataOnly).
// MetadataOnly strips prompts, messages, tool arguments, and tool results before
// export while preserving usage, timing, model info, and message structure.
// Per-generation and per-tool-execution overrides are available via the
// ContentCapture field on GenerationStart and ToolExecutionStart.
// Tool executions inherit the parent generation's mode via context.
//
// Config.ContentCaptureResolver enables dynamic per-request resolution. When set,
// the resolver is called before each generation start and rating submission with
// the request context and recording metadata. This is the recommended integration
// point for multi-tenant services that need to resolve content capture mode based
// on tenant-level policies (e.g., data-sharing opt-out).
//
// Resolution precedence (highest to lowest):
//   - Per-recording ContentCapture field (explicit override)
//   - ContentCaptureResolver return value
//   - Config.ContentCapture (static default)
//
// Resolver panics are recovered and treated as MetadataOnly (fail-closed).
//
// MetadataOnly does not filter user-provided Metadata or Tags maps.
// Callers are responsible for not including sensitive content in those fields
// when using MetadataOnly mode.
//
// # Linking
//
// Linking is bi-directional:
//   - Generation.TraceID/SpanID point to the created span.
//   - The span includes the generation ID in attribute "sigil.generation.id".
package sigil
