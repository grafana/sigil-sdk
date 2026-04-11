// Package genkit maps Genkit model middleware events to Sigil generation recorders.
//
// The integration uses Genkit's ModelMiddleware interface to intercept LLM calls
// and capture telemetry via sigil.Client. Both synchronous and streaming generation
// modes are supported, with first-token-at-time tracking for streams.
//
// Content capture (inputs and outputs) can be independently controlled via Options
// for privacy-sensitive deployments.
package genkit
