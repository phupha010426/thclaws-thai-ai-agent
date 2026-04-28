import { prisma } from '../../db/postgres.js';

export class PermissionService {
  async canAccessUserData(input: { actorUserId: string; ownerUserId: string }) {
    return input.actorUserId === input.ownerUserId;
  }

  async ensureDefaultConsent(userId: string) {
    await prisma.consent.upsert({
      where: {
        userId_purpose: {
          userId,
          purpose: 'line_accounting_mvp'
        }
      },
      update: {},
      create: {
        userId,
        purpose: 'line_accounting_mvp',
        status: 'GRANTED'
      }
    });
  }
}

export const permissionService = new PermissionService();
