import type { GenerationExportConfig, GenerationExporter } from '../types.js';
import { GRPCGenerationExporter } from './grpc.js';
import { HTTPGenerationExporter } from './http.js';

export function createDefaultGenerationExporter(config: GenerationExportConfig): GenerationExporter {
  switch (config.protocol) {
    case 'http':
      return new HTTPGenerationExporter(config.endpoint, config.headers);
    case 'grpc':
      return new GRPCGenerationExporter(config.endpoint, config.headers, config.insecure);
    default:
      return new UnavailableGenerationExporter(new Error(`unsupported generation export protocol: ${config.protocol as string}`));
  }
}

class UnavailableGenerationExporter implements GenerationExporter {
  constructor(private readonly reason: Error) {}

  async exportGenerations(): Promise<never> {
    throw this.reason;
  }
}
