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
// Linking is bi-directional:
//   - Generation.TraceID/SpanID point to the created span.
//   - The span includes the generation ID in attribute "sigil.generation.id".
package sigil
