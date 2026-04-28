import assert from 'node:assert/strict';
import test from 'node:test';

import { parseImageUnderstanding } from '../src/modules/agents/image-understanding.parser.js';

test('parses standard JSON object', () => {
  const { result, usedFallback } = parseImageUnderstanding('{"finalReply":"hi","isSlip":false,"confidence":0.9}');
  assert.equal(usedFallback, false);
  assert.equal(result.finalReply, 'hi');
  assert.equal(result.isSlip, false);
  assert.equal(result.confidence, 0.9);
});

test('parses Python repr dict (single quotes)', () => {
  const raw = "{'finalReply': 'อ่านสลิปแล้ว', 'isSlip': True, 'amount': 1234.5, 'currency': 'THB', 'directionHint': 'EXPENSE', 'confidence': 0.85}";
  const { result, usedFallback } = parseImageUnderstanding(raw);
  assert.equal(usedFallback, false);
  assert.equal(result.finalReply, 'อ่านสลิปแล้ว');
  assert.equal(result.isSlip, true);
  assert.equal(result.amount, 1234.5);
  assert.equal(result.directionHint, 'EXPENSE');
  assert.equal(result.confidence, 0.85);
});

test('parses chat-parts list wrapper with inner JSON string', () => {
  const raw = "[{'type': 'text', 'text': '{\"finalReply\":\"hi\",\"isSlip\":false,\"confidence\":0.5}'}]";
  const { result, usedFallback } = parseImageUnderstanding(raw);
  assert.equal(usedFallback, false);
  assert.equal(result.finalReply, 'hi');
});

test('falls back gracefully when chat-parts contains plain Thai text', () => {
  const raw = "[{'type': 'text', 'text': 'อาหารไทย'}]";
  const { result, usedFallback } = parseImageUnderstanding(raw);
  assert.equal(usedFallback, true);
  assert.equal(result.isSlip, false);
  assert.match(result.finalReply, /อาหารไทย/);
});

test('parses Python None as null and False as boolean', () => {
  const raw = "{'finalReply': 'ok', 'isSlip': False, 'amount': None, 'confidence': 0.4}";
  const { result } = parseImageUnderstanding(raw);
  assert.equal(result.amount, null);
  assert.equal(result.isSlip, false);
});

test('extracts JSON object embedded in prose', () => {
  const raw = `Here is the result: {"finalReply":"พบสลิป","isSlip":true,"amount":500,"confidence":0.7}. Done.`;
  const { result } = parseImageUnderstanding(raw);
  assert.equal(result.isSlip, true);
  assert.equal(result.amount, 500);
});

test('falls back gracefully when content is unparseable', () => {
  const raw = 'completely broken response';
  const { result, usedFallback } = parseImageUnderstanding(raw);
  assert.equal(usedFallback, true);
  assert.equal(result.isSlip, false);
  assert.equal(result.confidence, 0.3);
  assert.match(result.finalReply, /broken response|รับรูป/);
});
