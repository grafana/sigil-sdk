import assert from 'node:assert/strict';
import test from 'node:test';
import { SigilClient } from '../.test-dist/index.js';

test('invalid generation auth config throws at client init', () => {
  assert.throws(
    () =>
      new SigilClient({
        generationExport: {
          auth: {
            mode: 'tenant',
          },
        },
      }),
    /requires tenantId/
  );
});
