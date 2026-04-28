import type { TransactionType } from '@prisma/client';

export type Intent = 'record_expense' | 'record_income' | 'ask_today_summary' | 'ask_month_summary' | 'help' | 'unknown';

export type ParsedTransaction = {
  intent: 'record_expense' | 'record_income';
  type: TransactionType;
  amount: number;
  category: string;
  note: string;
  confidence: number;
};

export type ParsedIntent =
  | ParsedTransaction
  | { intent: 'ask_today_summary' | 'ask_month_summary' | 'help' | 'unknown'; reason?: string };

const incomeRules: Array<[RegExp, string]> = [
  [/เงินเดือน|salary/i, 'เงินเดือน'],
  [/ขายของ|ขายได้|ยอดขาย|ลูกค้าโอน|รับเงิน/i, 'รายได้จากการขาย'],
  [/โบนัส|รายได้|ได้เงิน/i, 'รายรับอื่น ๆ']
];

const expenseRules: Array<[RegExp, string]> = [
  [/ค่าไฟ|ไฟฟ้า|ค่าน้ำ|น้ำประปา/i, 'ค่าน้ำค่าไฟ'],
  [/น้ำมัน|รถ|แท็กซี่|รถเมล์|เดินทาง|ค่าโดยสาร/i, 'เดินทาง'],
  [/ซื้อของเข้าบ้าน|ของเข้าบ้าน|โลตัส|บิ๊กซี|ตลาด|ของใช้/i, 'ของใช้เข้าบ้าน'],
  [/กาแฟ|ชา|ข้าว|อาหาร|ก๋วยเตี๋ยว|น้ำดื่ม|ขนม|ผัก|หมู|ไก่|ไข่/i, 'อาหารและเครื่องดื่ม'],
  [/ค่าเทอม|เรียน|หนังสือ|โรงเรียน|การศึกษา/i, 'การศึกษา'],
  [/หมอ|ยา|โรงพยาบาล|สุขภาพ/i, 'สุขภาพ']
];

export class TransactionParserService {
  parse(text: string): ParsedIntent {
    const normalized = text.replace(/,/g, '').trim();
    if (!normalized) return { intent: 'unknown', reason: 'empty_text' };

    if (/^(ช่วยเหลือ|help|วิธีใช้)$/i.test(normalized)) return { intent: 'help' };
    if (/^(สรุปวันนี้|วันนี้ใช้ไปเท่าไร|วันนี้ใช้เท่าไร|วันนี้ใช้ไปกี่บาท)$/i.test(normalized)
      || /วันนี้.*(จ่าย|ใช้|หมด|เสีย|อะไร|ไปบ้าง|เท่าไร|กี่บาท)/i.test(normalized)) {
      return { intent: 'ask_today_summary' };
    }
    if (/^(สรุปเดือนนี้|เดือนนี้ใช้ไปเท่าไร|เดือนนี้ใช้เท่าไร|เดือนนี้ใช้ไปกี่บาท)$/i.test(normalized)
      || /เดือนนี้.*(จ่าย|ใช้|หมด|เสีย|อะไร|ไปบ้าง|เท่าไร|กี่บาท)/i.test(normalized)) {
      return { intent: 'ask_month_summary' };
    }

    const amountMatch = normalized.match(/(\d+(?:\.\d{1,2})?)/);
    if (!amountMatch) return { intent: 'unknown', reason: 'missing_amount' };

    const amount = Number(amountMatch[1]);
    if (!Number.isFinite(amount) || amount <= 0) return { intent: 'unknown', reason: 'invalid_amount' };

    const incomeMatch = incomeRules.find(([regex]) => regex.test(normalized));
    const expenseMatch = expenseRules.find(([regex]) => regex.test(normalized));
    const isIncome = Boolean(incomeMatch) && !/^ซื้อ|^จ่าย|^ค่า/.test(normalized);
    const category = isIncome ? incomeMatch?.[1] ?? 'รายรับอื่น ๆ' : expenseMatch?.[1] ?? 'อื่น ๆ';
    const note = normalized.replace(amountMatch[1], '').trim();
    const confidence = category === 'อื่น ๆ' || category === 'รายรับอื่น ๆ' ? 0.7 : 0.95;

    return {
      intent: isIncome ? 'record_income' : 'record_expense',
      type: isIncome ? 'INCOME' : 'EXPENSE',
      amount,
      category,
      note: note || normalized,
      confidence
    };
  }
}

export const transactionParserService = new TransactionParserService();
