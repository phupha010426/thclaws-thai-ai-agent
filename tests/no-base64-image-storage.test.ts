import assert from 'node:assert/strict';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

test('image base64 must not be persisted in schemas or storage models', () => {
  const files = listFiles('src')
    .filter((file) => file.endsWith('.ts'))
    .filter((file) => !file.endsWith(join('agents', 'image-understanding.service.ts')))
    .filter((file) => !file.endsWith(join('line', 'line-signature.service.ts')));
  const violations: string[] = [];

  for (const file of files) {
    const content = stripComments(readFileSync(file, 'utf8')).toLowerCase();
    if (content.includes('base64') || content.includes('data:image')) violations.push(file);
  }

  assert.deepEqual(violations, []);
});

function stripComments(content: string) {
  return content
    .replace(/\/\*[\s\S]*?\*\//g, '')
    .replace(/(^|\s)\/\/.*$/gm, '');
}

function listFiles(root: string): string[] {
  const entries = readdirSync(root);
  const files: string[] = [];
  for (const entry of entries) {
    const path = join(root, entry);
    const stat = statSync(path);
    if (stat.isDirectory()) files.push(...listFiles(path));
    else files.push(path);
  }
  return files;
}
