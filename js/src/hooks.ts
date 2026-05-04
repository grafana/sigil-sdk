import type {
  HookEvaluateRequest,
  HookEvaluateResponse,
  HookEvaluation,
  HookInput,
  HooksConfig,
} from './types.js';
import { asError } from './utils.js';

const hooksEvaluatePath = '/api/v1/hooks:evaluate';
const hookTimeoutHeader = 'X-Sigil-Hook-Timeout-Ms';

/**
 * Thrown by framework adapters when hook evaluation returns `action: 'deny'`.
 *
 * The error preserves the rule that triggered the deny and the per-rule
 * evaluation outcomes so callers can build user-facing error messages.
 */
export class HookDeniedError extends Error {
  readonly action = 'deny' as const;
  readonly reason: string;
  readonly ruleId?: string;
  readonly evaluations: HookEvaluation[];

  constructor(reason: string, ruleId?: string, evaluations: HookEvaluation[] = []) {
    super(formatDenyMessage(reason, ruleId));
    this.name = 'HookDeniedError';
    const normalized = reason?.trim() ?? '';
    this.reason = normalized.length > 0 ? normalized : 'request blocked by Sigil hook rule';
    this.ruleId = ruleId;
    this.evaluations = evaluations;
  }
}

/**
 * Sends a hook evaluation request to the Sigil API.
 *
 * `apiEndpoint` is the Sigil API base URL (without the `/api/v1/...` suffix).
 * `extraHeaders` is merged into the request — typically the same auth headers
 * the SDK uses for generation export.
 *
 * Returns `{ action: 'allow', evaluations: [] }` when the request fails and
 * `hooks.failOpen` is true.
 */
export async function evaluateHook(params: {
  apiEndpoint: string;
  insecure: boolean;
  extraHeaders: Record<string, string> | undefined;
  hooks: HooksConfig;
  request: HookEvaluateRequest;
  fetchImpl?: typeof fetch;
}): Promise<HookEvaluateResponse> {
  const fetchImpl = params.fetchImpl ?? fetch;
  if (!params.hooks.enabled) {
    return allowResponse();
  }

  const phases = params.hooks.phases;
  if (phases.length > 0 && !phases.includes(params.request.phase)) {
    return allowResponse();
  }

  const timeoutMs = params.hooks.timeoutMs > 0 ? params.hooks.timeoutMs : 15_000;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);

  try {
    const url = buildHooksEvaluateEndpoint(params.apiEndpoint, params.insecure);
    const body = serializeRequest(params.request);

    const response = await fetchImpl(url, {
      method: 'POST',
      signal: controller.signal,
      headers: {
        'content-type': 'application/json',
        [hookTimeoutHeader]: String(timeoutMs),
        ...(params.extraHeaders ?? {}),
      },
      body: JSON.stringify(body),
    });

    const responseText = (await response.text()).trim();
    if (!response.ok) {
      return failOpenOrThrow(
        params.hooks.failOpen,
        new Error(
          `sigil hook evaluation failed: status ${response.status}: ${hookErrorText(responseText, response.status)}`,
        ),
      );
    }
    if (responseText.length === 0) {
      return failOpenOrThrow(params.hooks.failOpen, new Error('sigil hook evaluation failed: empty response payload'));
    }

    let payload: unknown;
    try {
      payload = JSON.parse(responseText);
    } catch (error) {
      return failOpenOrThrow(
        params.hooks.failOpen,
        new Error(`sigil hook evaluation failed: invalid JSON response: ${asError(error).message}`),
      );
    }

    return parseEvaluateResponse(payload);
  } catch (error) {
    return failOpenOrThrow(params.hooks.failOpen, asError(error));
  } finally {
    clearTimeout(timer);
  }
}

function failOpenOrThrow(failOpen: boolean, error: Error): HookEvaluateResponse {
  if (failOpen) {
    return allowResponse();
  }
  throw error;
}

function allowResponse(): HookEvaluateResponse {
  return { action: 'allow', evaluations: [] };
}

function formatDenyMessage(reason: string, ruleId: string | undefined): string {
  const trimmedReason = reason?.trim() ?? '';
  const baseReason = trimmedReason.length > 0 ? trimmedReason : 'request blocked by Sigil hook rule';
  if (ruleId !== undefined && ruleId.length > 0) {
    return `sigil hook denied by rule ${ruleId}: ${baseReason}`;
  }
  return `sigil hook denied: ${baseReason}`;
}

function buildHooksEvaluateEndpoint(endpoint: string, insecure: boolean): string {
  const baseURL = baseURLFromAPIEndpoint(endpoint, insecure);
  return `${baseURL}${hooksEvaluatePath}`;
}

function baseURLFromAPIEndpoint(endpoint: string, insecure: boolean): string {
  const trimmed = endpoint.trim();
  if (trimmed.length === 0) {
    throw new Error('sigil hook evaluation failed: api endpoint is required');
  }

  if (trimmed.startsWith('http://') || trimmed.startsWith('https://')) {
    const parsed = new URL(trimmed);
    return `${parsed.protocol}//${parsed.host}`;
  }

  const withoutScheme = trimmed.startsWith('grpc://') ? trimmed.slice('grpc://'.length) : trimmed;
  const host = withoutScheme.split('/')[0]?.trim();
  if (host === undefined || host.length === 0) {
    throw new Error('sigil hook evaluation failed: api endpoint host is required');
  }
  return `${insecure ? 'http' : 'https'}://${host}`;
}

function serializeRequest(request: HookEvaluateRequest): Record<string, unknown> {
  const body: Record<string, unknown> = {
    phase: request.phase,
    context: serializeContext(request.context),
    input: serializeInput(request.input),
  };
  return body;
}

function serializeContext(context: HookEvaluateRequest['context']): Record<string, unknown> {
  const out: Record<string, unknown> = {
    model: { provider: context.model.provider, name: context.model.name },
  };
  if (context.agentName !== undefined && context.agentName.length > 0) {
    out.agent_name = context.agentName;
  }
  if (context.agentVersion !== undefined && context.agentVersion.length > 0) {
    out.agent_version = context.agentVersion;
  }
  if (context.tags !== undefined && Object.keys(context.tags).length > 0) {
    out.tags = { ...context.tags };
  }
  return out;
}

function serializeInput(input: HookEvaluateRequest['input']): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  if (input.messages !== undefined && input.messages.length > 0) {
    out.messages = input.messages;
  }
  if (input.tools !== undefined && input.tools.length > 0) {
    out.tools = input.tools;
  }
  if (input.systemPrompt !== undefined && input.systemPrompt.length > 0) {
    out.system_prompt = input.systemPrompt;
  }
  if (input.output !== undefined && input.output.length > 0) {
    out.output = input.output;
  }
  if (input.conversationPreview !== undefined && input.conversationPreview.length > 0) {
    out.conversation_preview = input.conversationPreview;
  }
  return out;
}

function parseEvaluateResponse(payload: unknown): HookEvaluateResponse {
  if (!isRecord(payload)) {
    throw new Error('sigil hook evaluation failed: invalid response payload');
  }
  const action = payload.action === 'deny' ? 'deny' : 'allow';
  const ruleId = typeof payload.rule_id === 'string' ? payload.rule_id : undefined;
  const reason = typeof payload.reason === 'string' ? payload.reason : undefined;

  const rawEvaluations = Array.isArray(payload.evaluations) ? payload.evaluations : [];
  const evaluations: HookEvaluation[] = [];
  for (const entry of rawEvaluations) {
    if (!isRecord(entry)) {
      continue;
    }
    evaluations.push({
      ruleId: stringField(entry.rule_id),
      evaluatorId: stringField(entry.evaluator_id),
      evaluatorKind: stringField(entry.evaluator_kind),
      passed: entry.passed === true,
      latencyMs: numberField(entry.latency_ms),
      explanation: optionalStringField(entry.explanation),
      reason: optionalStringField(entry.reason),
    });
  }

  const transformedInput = parseTransformedInputPayload(payload);

  return { action, ruleId, reason, transformedInput, evaluations };
}

function parseTransformedInputPayload(payload: Record<string, unknown>): HookInput | undefined {
  const raw = payload.transformed_input;
  if (raw === undefined || raw === null) {
    return undefined;
  }
  if (!isRecord(raw)) {
    return undefined;
  }
  const out: HookInput = {};
  if (Array.isArray(raw.messages) && raw.messages.length > 0) {
    out.messages = raw.messages as HookInput['messages'];
  }
  if (Array.isArray(raw.tools) && raw.tools.length > 0) {
    out.tools = raw.tools as HookInput['tools'];
  }
  if (typeof raw.system_prompt === 'string' && raw.system_prompt.length > 0) {
    out.systemPrompt = raw.system_prompt;
  }
  if (Array.isArray(raw.output) && raw.output.length > 0) {
    out.output = raw.output as HookInput['output'];
  }
  if (typeof raw.conversation_preview === 'string' && raw.conversation_preview.length > 0) {
    out.conversationPreview = raw.conversation_preview;
  }
  if (
    out.messages === undefined &&
    out.tools === undefined &&
    out.systemPrompt === undefined &&
    out.output === undefined &&
    out.conversationPreview === undefined
  ) {
    return undefined;
  }
  return out;
}

function stringField(value: unknown): string {
  return typeof value === 'string' ? value : '';
}

function optionalStringField(value: unknown): string | undefined {
  if (typeof value !== 'string' || value.length === 0) {
    return undefined;
  }
  return value;
}

function numberField(value: unknown): number {
  if (typeof value === 'number' && Number.isFinite(value)) {
    return value;
  }
  return 0;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value);
}

function hookErrorText(body: string, status: number): string {
  if (body.length > 0) {
    return body;
  }
  return `HTTP ${status}`;
}
