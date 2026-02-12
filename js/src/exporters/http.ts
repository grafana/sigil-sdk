import type { ExportGenerationResult, ExportGenerationsRequest, ExportGenerationsResponse, GenerationExporter } from '../types.js';
import { isRecord } from '../utils.js';

export class HTTPGenerationExporter implements GenerationExporter {
  private readonly endpoint: string;
  private readonly headers: Record<string, string>;

  constructor(endpoint: string, headers?: Record<string, string>) {
    this.endpoint = normalizeHTTPGenerationEndpoint(endpoint);
    this.headers = headers ? { ...headers } : {};
  }

  async exportGenerations(request: ExportGenerationsRequest): Promise<ExportGenerationsResponse> {
    const response = await fetch(this.endpoint, {
      method: 'POST',
      headers: {
        'content-type': 'application/json',
        ...this.headers,
      },
      body: JSON.stringify(request),
    });

    if (!response.ok) {
      const responseText = (await response.text()).trim();
      throw new Error(`http generation export status ${response.status}: ${responseText}`);
    }

    const payload = (await response.json()) as unknown;
    return parseExportGenerationsResponse(payload, request);
  }
}

function parseExportGenerationsResponse(
  payload: unknown,
  request: ExportGenerationsRequest
): ExportGenerationsResponse {
  if (!isRecord(payload) || !Array.isArray(payload.results)) {
    throw new Error('invalid generation export response payload');
  }

  const results: ExportGenerationResult[] = payload.results.map((result, index) => {
    if (!isRecord(result)) {
      throw new Error('invalid generation export result payload');
    }

    const fallbackGenerationID = request.generations[index]?.id ?? '';
    return {
      generationId:
        typeof result.generationId === 'string'
          ? result.generationId
          : typeof result.generation_id === 'string'
            ? result.generation_id
            : fallbackGenerationID,
      accepted: Boolean(result.accepted),
      error: typeof result.error === 'string' ? result.error : undefined,
    };
  });

  return { results };
}

function normalizeHTTPGenerationEndpoint(endpoint: string): string {
  const trimmed = endpoint.trim();
  if (trimmed.length === 0) {
    throw new Error('generation export endpoint is required');
  }

  if (trimmed.startsWith('http://') || trimmed.startsWith('https://')) {
    return trimmed;
  }
  return `http://${trimmed}`;
}
