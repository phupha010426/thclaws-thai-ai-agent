import { Prisma, TransactionType } from '@prisma/client';
import { prisma } from '../../db/postgres.js';
import { auditService } from '../audit/audit.service.js';
import { FinancialTransaction } from '../memory/memory.model.js';
import { logger } from '../../libs/logger.js';

export class LedgerService {
  async createPersonalTransaction(input: {
    userId: string;
    agentId: string;
    type: TransactionType;
    amount: number;
    category: string;
    note?: string;
    confidence?: number;
  }) {
    const mongoTransaction = await FinancialTransaction.create({
      ownerId: input.userId,
      agentId: input.agentId,
      namespace: `ledger:user:${input.userId}`,
      type: input.type,
      amount: input.amount,
      category: input.category,
      note: input.note,
      source: 'line',
      confidence: input.confidence ?? 1
    });

    const pgTransaction = await prisma.transaction.create({
      data: {
        userId: input.userId,
        agentId: input.agentId,
        type: input.type,
        amount: new Prisma.Decimal(input.amount),
        category: input.category,
        note: input.note,
        confidence: input.confidence ?? 1,
        source: 'line'
      }
    });

    await FinancialTransaction.updateOne(
      { _id: mongoTransaction._id },
      { $set: { pgTransactionId: pgTransaction.id } }
    );

    await auditService.log({
      actorId: input.userId,
      action: 'ledger.transaction_created',
      resource: 'transactions',
      metadata: {
        transactionId: pgTransaction.id,
        mongoTransactionId: String(mongoTransaction._id),
        type: input.type,
        amount: input.amount,
        category: input.category
      }
    });
    logger.info({
      userId: input.userId,
      transactionId: pgTransaction.id,
      mongoTransactionId: String(mongoTransaction._id),
      ledgerNamespace: `ledger:user:${input.userId}`,
      type: input.type,
      amount: input.amount,
      category: input.category
    }, 'ledger transaction persisted in mongodb and mirrored to postgres');

    return { mongoTransaction, pgTransaction };
  }
}

export const ledgerService = new LedgerService();
