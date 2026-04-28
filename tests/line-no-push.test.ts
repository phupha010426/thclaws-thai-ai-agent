import assert from 'node:assert/strict';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

const forbiddenLineSendMethods = [
  'pushMessage',
  'multicast',
  'broadcast'
];

test('LINE OA must not push, multicast, or broadcast messages', () => {
  const files = listFiles('src').filter((file) => file.endsWith('.ts'));
  const violations: string[] = [];

  for (const file of files) {
    const content = stripComments(readFileSync(file, 'utf8'));
    for (const method of forbiddenLineSendMethods) {
      if (content.includes(method)) violations.push(`${file}: ${method}`);
    }
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
