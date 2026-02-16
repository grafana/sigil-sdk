import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import path from 'node:path';
import test from 'node:test';
import { fileURLToPath } from 'node:url';

const __dirname = path.dirname(fileURLToPath(import.meta.url));

test('sdk js package declares local TypeScript compiler tooling', async () => {
  const packageJsonPath = path.join(__dirname, '..', 'package.json');
  const packageJsonRaw = await readFile(packageJsonPath, 'utf8');
  const packageJson = JSON.parse(packageJsonRaw);

  assert.equal(
    typeof packageJson.devDependencies?.typescript,
    'string',
    'sdks/js must declare a local typescript dependency'
  );
});
