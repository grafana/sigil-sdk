// Package sigil provides manual recording helpers for LLM generations.
//
// The primary APIs are Client.StartGeneration/StartStreamingGeneration + GenerationRecorder.End:
// start returns a context for your provider call and End closes the GenAI span
// after you map the final normalized Generation with distinct Input and Output messages.
//
// Linking is bi-directional:
//   - Generation.TraceID/SpanID point to the created span.
//   - The span includes the generation ID in attribute "sigil.generation.id".
package sigil
