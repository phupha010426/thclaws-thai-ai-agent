import assert from 'node:assert/strict';
import test from 'node:test';
import { redactPii, containsPii } from '../src/modules/brain/redaction.js';

test('redacts Thai national ID', () => {
  const text = 'เลขบัตรประชาชน 1-2345-67890-12-3 ของผม';
  const out = redactPii(text);
  assert.match(out, /<id>/);
  assert.doesNotMatch(out, /12345/);
});

test('redacts bank account number', () => {
  const text = 'โอนเข้า 123-4-56789-0';
  const out = redactPii(text);
  assert.match(out, /<acc>/);
});

test('redacts mobile phone', () => {
  const text = 'โทรหา 081-234-5678 ได้';
  const out = redactPii(text);
  assert.match(out, /<phone>/);
});

test('redacts email', () => {
  const text = 'mail me at jaturapornchai@gmail.com';
  const out = redactPii(text);
  assert.match(out, /<email>/);
  assert.doesNotMatch(out, /jaturapornchai/);
});

test('preserves Thai food/expense text intact', () => {
  const text = 'กาแฟ 60 บาท';
  assert.equal(redactPii(text), text);
  assert.equal(containsPii(text), false);
});

test('containsPii reports correctly', () => {
  assert.equal(containsPii('โทร 081-234-5678'), true);
  assert.equal(containsPii('ไม่กินเผ็ด'), false);
});
