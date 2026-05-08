#!/usr/bin/env node
import { copyFile, mkdir, readdir } from 'node:fs/promises';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(__dirname, '..', '..');
const src = join(repoRoot, 'proto', 'sigil', 'v1');
const dst = join(__dirname, '..', 'proto', 'sigil', 'v1');

await mkdir(dst, { recursive: true });
const entries = await readdir(src);
for (const name of entries) {
  if (!name.endsWith('.proto')) continue;
  await copyFile(join(src, name), join(dst, name));
}
