import { prisma } from '../../db/postgres.js';
import { auditService } from '../audit/audit.service.js';
import { permissionService } from '../permissions/permission.service.js';

export class UserService {
  async findOrCreateLineUser(input: { lineUserId: string; displayName?: string }) {
    const user = await prisma.user.upsert({
      where: { lineUserId: input.lineUserId },
      update: { displayName: input.displayName },
      create: {
        lineUserId: input.lineUserId,
        displayName: input.displayName
      }
    });

    await permissionService.ensureDefaultConsent(user.id);
    await auditService.log({
      actorId: user.id,
      action: 'user.line_identified',
      resource: 'users',
      metadata: { lineUserId: input.lineUserId }
    });

    return user;
  }
}

export const userService = new UserService();
