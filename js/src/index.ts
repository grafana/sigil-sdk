export type SigilSdkConfig = {
  otlpHttpEndpoint: string;
  recordsEndpoint: string;
  payloadMaxBytes?: number;
};

export function createSigilClient(config: SigilSdkConfig) {
  return { config };
}
