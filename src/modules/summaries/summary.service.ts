import { TransactionType } from '@prisma/client';
import { prisma } from '../../db/postgres.js';
import { getLocalDayRange, getLocalMonthRange } from '../../utils/date.js';

export class SummaryService {
  async today(userId: string) {
    const { start, end } = getLocalDayRange();
    return this.buildSummary(userId, start, end, 'วันนี้');
  }

  async thisMonth(userId: string) {
    const { start, end } = getLocalMonthRange();
    return this.buildSummary(userId, start, end, 'เดือนนี้');
  }

  private async buildSummary(userId: string, start: Date, end: Date, label: string) {
    const rows = await prisma.transaction.findMany({
      where: { userId, happenedAt: { gte: start, lte: end } },
      select: { type: true, amount: true, category: true },
      orderBy: { happenedAt: 'desc' }
    });

    const expense = sumByType(rows, 'EXPENSE');
    const income = sumByType(rows, 'INCOME');
    const topExpense = topExpenseCategory(rows);

    if (label === 'วันนี้') {
      return topExpense
        ? `วันนี้ใช้ไป ${formatMoney(expense)} บาท มากสุดคือ${topExpense.category} ${formatMoney(topExpense.amount)} บาท`
        : `วันนี้ใช้ไป ${formatMoney(expense)} บาท`;
    }

    const balance = income - expense;
    return topExpense
      ? `เดือนนี้ใช้ไป ${formatMoney(expense)} บาท รายจ่ายมากสุดคือ${topExpense.category} ${formatMoney(topExpense.amount)} บาท คงเหลือ ${formatMoney(balance)} บาท`
      : `เดือนนี้ใช้ไป ${formatMoney(expense)} บาท คงเหลือ ${formatMoney(balance)} บาท`;
  }
}

type SummaryRow = { type: TransactionType; amount: unknown; category: string };

function sumByType(rows: SummaryRow[], type: TransactionType) {
  return rows.filter((row) => row.type === type).reduce((sum, row) => sum + Number(row.amount), 0);
}

function topExpenseCategory(rows: SummaryRow[]) {
  const values = rows
    .filter((row) => row.type === 'EXPENSE')
    .reduce<Record<string, number>>((acc, row) => {
      acc[row.category] = (acc[row.category] ?? 0) + Number(row.amount);
      return acc;
    }, {});
  const [category, amount] = Object.entries(values).sort((a, b) => b[1] - a[1])[0] ?? [];
  return category ? { category, amount } : null;
}

function formatMoney(value: number) {
  return value.toLocaleString('th-TH', { maximumFractionDigits: 2 });
}

export const summaryService = new SummaryService();
