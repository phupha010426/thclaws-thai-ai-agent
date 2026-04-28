import { AgentProgram } from '../memory/memory.model.js';

export type AgentProgramLanguage = 'typescript' | 'jsonlogic' | 'prompt';

export class AgentProgramService {
  async upsertProgram(input: {
    ownerId: string;
    agentId: string;
    namespace: string;
    name: string;
    description: string;
    language: AgentProgramLanguage;
    source: string;
  }) {
    const name = sanitizeProgramName(input.name);
    return AgentProgram.findOneAndUpdate(
      { namespace: input.namespace, name },
      {
        $set: {
          ownerId: input.ownerId,
          agentId: input.agentId,
          namespace: input.namespace,
          name,
          description: input.description.slice(0, 1000),
          language: input.language,
          source: input.source.slice(0, 20000),
          status: 'DRAFT',
          createdBy: 'thclaws'
        }
      },
      { upsert: true, new: true }
    );
  }
}

function sanitizeProgramName(value: string) {
  const normalized = value.trim().toLowerCase().replace(/[^a-z0-9_-]+/g, '_').replace(/^_+|_+$/g, '');
  if (!normalized) throw new Error('invalid agent program name');
  return normalized.slice(0, 80);
}

export const agentProgramService = new AgentProgramService();
