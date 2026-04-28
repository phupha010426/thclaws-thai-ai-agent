import assert from 'node:assert/strict';
import test from 'node:test';
import { transactionParserService } from '../src/modules/ledger/transaction-parser.service.js';

const cases = [
  ['กาแฟ 60', 'record_expense', 'EXPENSE', 60, 'อาหารและเครื่องดื่ม'],
  ['ข้าว 55', 'record_expense', 'EXPENSE', 55, 'อาหารและเครื่องดื่ม'],
  ['ค่าไฟ 980', 'record_expense', 'EXPENSE', 980, 'ค่าน้ำค่าไฟ'],
  ['ค่าน้ำ 120', 'record_expense', 'EXPENSE', 120, 'ค่าน้ำค่าไฟ'],
  ['เงินเดือน 25000', 'record_income', 'INCOME', 25000, 'เงินเดือน'],
  ['ขายของ 1500', 'record_income', 'INCOME', 1500, 'รายได้จากการขาย'],
  ['ซื้อของเข้าบ้าน 1200', 'record_expense', 'EXPENSE', 1200, 'ของใช้เข้าบ้าน'],
  ['น้ำมันรถ 700', 'record_expense', 'EXPENSE', 700, 'เดินทาง'],
  ['ค่าหมอ 450', 'record_expense', 'EXPENSE', 450, 'สุขภาพ'],
  ['โบนัส 3000', 'record_income', 'INCOME', 3000, 'รายรับอื่น ๆ']
] as const;

for (const [text, intent, type, amount, category] of cases) {
  test(`parse ${text}`, () => {
    const parsed = transactionParserService.parse(text);
    assert.equal(parsed.intent, intent);
    if (parsed.intent !== 'record_expense' && parsed.intent !== 'record_income') throw new Error('expected transaction intent');
    assert.equal(parsed.type, type);
    assert.equal(parsed.amount, amount);
    assert.equal(parsed.category, category);
  });
}

test('missing amount asks back', () => {
  const parsed = transactionParserService.parse('กาแฟ');
  assert.equal(parsed.intent, 'unknown');
  if (parsed.intent === 'unknown') assert.equal(parsed.reason, 'missing_amount');
});

test('summary intents', () => {
  assert.equal(transactionParserService.parse('สรุปวันนี้').intent, 'ask_today_summary');
  assert.equal(transactionParserService.parse('วันนี้จ่ายค่าอะไรไปบ้าง').intent, 'ask_today_summary');
  assert.equal(transactionParserService.parse('วันนี้ใช้ไปทั้งหมดเท่าไร').intent, 'ask_today_summary');
  assert.equal(transactionParserService.parse('สรุปเดือนนี้').intent, 'ask_month_summary');
  assert.equal(transactionParserService.parse('เดือนนี้จ่ายค่าอะไรไปบ้าง').intent, 'ask_month_summary');
  assert.equal(transactionParserService.parse('ช่วยเหลือ').intent, 'help');
});
