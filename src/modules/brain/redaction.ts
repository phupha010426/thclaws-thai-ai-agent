/**
 * Strip personally identifiable information before storing or embedding.
 * The ledger is the source of truth for amounts; embeddings only need
 * semantic content, so we replace identifier-shaped strings with typed
 * placeholders (`<acc>`, `<id>`, `<phone>`, `<email>`). Lossy by design.
 *
 * This is a defence-in-depth layer — not a substitute for proper PDPA
 * delete/export endpoints — but it caps the blast radius if a vector or
 * raw row leaks elsewhere.
 */

const PATTERNS: Array<{ name: string; regex: RegExp; placeholder: string }> = [
  // Thai national ID — 13 digits, possibly with hyphens.
  { name: 'thai_id', regex: /\b\d[-\s]?\d{4}[-\s]?\d{5}[-\s]?\d{2}[-\s]?\d\b/g, placeholder: '<id>' },
  // Bank account — 10-13 digits with hyphens.
  { name: 'account', regex: /\b\d{3}[-\s]\d[-\s]\d{4,6}[-\s]\d{1,3}\b/g, placeholder: '<acc>' },
  // Mobile / landline (Thai).
  { name: 'phone', regex: /\b0\d{1,2}[-\s]?\d{3}[-\s]?\d{4}\b/g, placeholder: '<phone>' },
  // Email.
  { name: 'email', regex: /[\w.+-]+@[\w-]+\.[\w.-]+/g, placeholder: '<email>' },
  // Credit-card-ish (13-19 digits with optional separators).
  { name: 'cc', regex: /\b(?:\d[ -]?){13,19}\b/g, placeholder: '<cc>' }
];

export function redactPii(text: string): string {
  let out = text;
  for (const { regex, placeholder } of PATTERNS) {
    out = out.replace(regex, placeholder);
  }
  return out;
}

/** Returns true when the string differs after redaction (i.e. PII was found). */
export function containsPii(text: string): boolean {
  return redactPii(text) !== text;
}
