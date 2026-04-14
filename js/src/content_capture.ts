import type { ContentCaptureMode, ContentCaptureResolver, Generation, Message } from './types.js';

export const metadataKeyContentCaptureMode = 'sigil.sdk.content_capture_mode';

/**
 * Returns the effective mode from an override and a fallback. `'default'` is
 * transparent — it falls through to the fallback.
 */
export function resolveContentCaptureMode(
  override: ContentCaptureMode,
  fallback: ContentCaptureMode,
): ContentCaptureMode {
  if (override !== 'default') {
    return override;
  }
  return fallback;
}

/**
 * Resolves the effective mode at the client level. `'default'` resolves to
 * `'no_tool_content'` for backward compatibility.
 */
export function resolveClientContentCaptureMode(mode: ContentCaptureMode): ContentCaptureMode {
  if (mode === 'default') {
    return 'no_tool_content';
  }
  return mode;
}

/**
 * Invokes the resolver callback safely, catching thrown errors. Returns
 * `'default'` when the resolver is undefined. Errors are treated as
 * `'metadata_only'` (fail-closed).
 */
export function callContentCaptureResolver(
  resolver: ContentCaptureResolver | undefined,
  metadata: Record<string, unknown> | undefined,
): ContentCaptureMode {
  if (resolver === undefined) {
    return 'default';
  }
  try {
    return resolver(metadata);
  } catch {
    return 'metadata_only';
  }
}

/** Sets the content capture mode marker on the generation metadata. */
export function stampContentCaptureMetadata(generation: Generation, mode: ContentCaptureMode): void {
  if (generation.metadata === undefined) {
    generation.metadata = {};
  }
  generation.metadata[metadataKeyContentCaptureMode] = mode;
}

/**
 * Strips sensitive content from a generation while preserving message
 * structure (roles, part types), tool names/IDs, usage, timing, and all
 * other metadata fields. `errorCategory` is the classified error category
 * used to replace the raw `callError` text.
 */
export function stripContent(generation: Generation, errorCategory: string): void {
  generation.systemPrompt = '';
  generation.conversationTitle = '';
  generation.artifacts = null;

  if (generation.callError !== undefined) {
    generation.callError = errorCategory.length > 0 ? errorCategory : 'sdk_error';
  }
  if (generation.metadata !== undefined) {
    delete generation.metadata.call_error;
    delete generation.metadata['sigil.conversation.title'];
  }

  if (generation.input !== undefined) {
    for (const message of generation.input) {
      stripMessageContent(message);
    }
  }
  if (generation.output !== undefined) {
    for (const message of generation.output) {
      stripMessageContent(message);
    }
  }
  if (generation.tools !== undefined) {
    for (const tool of generation.tools) {
      tool.description = '';
      tool.inputSchemaJSON = '';
    }
  }
}

function stripMessageContent(message: Message): void {
  if (message.parts === undefined) {
    message.content = '';
    return;
  }
  message.content = '';
  for (const part of message.parts) {
    switch (part.type) {
      case 'text':
        part.text = '';
        break;
      case 'thinking':
        part.thinking = '';
        break;
      case 'tool_call':
        part.toolCall.inputJSON = '';
        break;
      case 'tool_result':
        part.toolResult.content = '';
        part.toolResult.contentJSON = '';
        break;
    }
  }
}

/**
 * Determines whether tool execution content (arguments, results) should be
 * included in span attributes. Resolves the effective mode from the
 * explicit tool override, client default, resolver, and legacy
 * `includeContent`.
 */
export function shouldIncludeToolContent(
  toolMode: ContentCaptureMode,
  clientDefault: ContentCaptureMode,
  resolverMode: ContentCaptureMode,
  legacyInclude: boolean,
): boolean {
  let resolved = resolveClientContentCaptureMode(resolveContentCaptureMode(resolverMode, clientDefault));
  if (toolMode !== 'default') {
    resolved = toolMode;
  }
  switch (resolved) {
    case 'metadata_only':
      return false;
    case 'full':
      return true;
    default:
      return legacyInclude;
  }
}
