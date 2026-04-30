// Strip ambient SIGIL_* / OTEL_* env vars; SigilClient reads them automatically.
for (const key of Object.keys(process.env)) {
  if (key.startsWith('SIGIL_') || key.startsWith('OTEL_')) {
    delete process.env[key];
  }
}
