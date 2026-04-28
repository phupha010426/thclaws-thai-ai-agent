import { AgentType } from '@prisma/client';
import { prisma } from '../../db/postgres.js';
import { auditService } from '../audit/audit.service.js';

export class AgentService {
  async ensurePersonalAgent(userId: string) {
    const existing = await prisma.agent.findFirst({
      where: { ownerId: userId, type: AgentType.PERSONAL, isActive: true },
      orderBy: { createdAt: 'asc' }
    });
    if (existing) return existing;

    const agent = await prisma.agent.create({
      data: {
        ownerId: userId,
        type: AgentType.PERSONAL,
        name: 'ThaiAiAgent ส่วนตัว',
        namespace: `memory:user:${userId}`
      }
    });

    await auditService.log({
      actorId: userId,
      action: 'agent.personal_created',
      resource: 'agents',
      metadata: { agentId: agent.id }
    });

    return agent;
  }
}

export const agentService = new AgentService();
