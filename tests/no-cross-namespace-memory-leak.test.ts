import assert from 'node:assert/strict';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import test from 'node:test';

/**
 * Lint-style guard: every Prisma query against any namespace-scoped table
 * must include a `namespace` filter. This prevents accidental cross-tenant
 * reads/writes for the LINE userId-isolated brain (wiki_pages, wiki_aliases,
 * wiki_edges, memory_embeddings, ledger_events).
 *
 * The Postgres triggers in the wiki migration are the last line of defense;
 * this test is the first.
 */

const NAMESPACE_TABLES = [
  'wikiPage',
  'wikiAlias',
  'wikiEdge',
  'memoryEmbedding',
  'ledgerEvent'
];

const QUERY_METHODS = [
  'findFirst',
  'findMany',
  'findUnique',
  'count',
  'update',
  'updateMany',
  'delete',
  'deleteMany',
  'aggregate'
];

test('every namespace-scoped Prisma query includes a namespace filter', () => {
  const files = listFiles('src').filter((file) => file.endsWith('.ts'));
  const violations: string[] = [];

  for (const file of files) {
    const content = readFileSync(file, 'utf8');
    for (const table of NAMESPACE_TABLES) {
      for (const method of QUERY_METHODS) {
        const pattern = new RegExp(`\\.${table}\\.${method}\\s*\\(([\\s\\S]*?)\\)`, 'g');
        for (const match of content.matchAll(pattern)) {
          const args = match[1] ?? '';
          if (!args.includes('namespace')) {
            violations.push(`${file}: ${table}.${method} without namespace filter`);
          }
        }
      }
    }
  }

  assert.deepEqual(violations, []);
});

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
