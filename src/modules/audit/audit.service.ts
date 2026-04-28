import { prisma } from '../../db/postgres.js';
import { logger } from '../../libs/logger.js';

export type AuditMetadata = Record<string, string | number | boolean | null | undefined>;

export class AuditService {
  async log(input: { actorId?: string; action: string; resource: string; metadata?: AuditMetadata }) {
    try {
      await prisma.auditLog.create({
        data: {
          actorId: input.actorId,
          action: input.action,
          resource: input.resource,
          metadata: sanitizeMetadata(input.metadata ?? {})
        }
      });
    } catch (error) {
      logger.error({ error, action: input.action, resource: input.resource }, 'audit log failed');
    }
  }
}

function sanitizeMetadata(metadata: AuditMetadata) {
  return Object.fromEntries(
    Object.entries(metadata).filter(([key]) => !/token|secret|password|authorization/i.test(key))
  );
}

export const auditService = new AuditService();
