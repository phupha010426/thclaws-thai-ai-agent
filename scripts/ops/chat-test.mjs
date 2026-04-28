import { connectMongo } from './dist/db/mongo.js';
import { prisma } from './dist/db/postgres.js';
import { userService } from './dist/modules/users/user.service.js';
import { agentService } from './dist/modules/agents/agent.service.js';
import { thClawsAgentService } from './dist/modules/agents/thclaws-agent.service.js';
import { memoryService } from './dist/modules/memory/memory.service.js';

const prompts = [
  'สวัสดี ผมชื่อจืด ทดสอบระบบ',
  'ขอสูตรส้มตำหน่อย',
  'กาแฟ 60',
  'วันนี้จ่ายค่าอะไรไปบ้าง',
  'จำไว้ว่าผมไม่กินเผ็ด',
  'เมื่อกี้ผมบอกว่าไม่กินอะไร',
  'เดือนนี้ใช้ไปเท่าไร',
  'ช่วยวางแผนเตรียมของไปปิ้งย่างเย็นนี้แบบสั้นๆ'
];

await connectMongo();

const lineUserId = `codex-log-check-${Date.now()}`;
const user = await userService.findOrCreateLineUser({ lineUserId });
const agent = await agentService.ensurePersonalAgent(user.id);
await memoryService.ensureAgentProfile(agent.id, agent.namespace);

console.log(JSON.stringify({ namespace: agent.namespace, lineUserId }));

for (const text of prompts) {
  await memoryService.saveConversationMessage({
    ownerId: user.id,
    agentId: agent.id,
    namespace: agent.namespace,
    role: 'user',
    content: text,
    metadata: { source: 'codex_test' }
  });
  try {
    const plan = await thClawsAgentService.planTextMessage({
      userId: user.id,
      agentId: agent.id,
      namespace: agent.namespace,
      text
    });
    const actionTypes = plan.actions.map((action) => action.type);
    console.log(JSON.stringify({ text, actionTypes, finalReply: plan.finalReply ?? null }));
    for (const action of plan.actions) {
      if (action.type === 'remember_memory') {
        await memoryService.rememberImportantFact({
          ownerId: user.id,
          agentId: agent.id,
          namespace: agent.namespace,
          content: action.content,
          source: 'codex_test'
        });
      }
      if (action.type === 'update_wiki') {
        await memoryService.upsertWikiPage({
          ownerId: user.id,
          agentId: agent.id,
          namespace: agent.namespace,
          title: action.title,
          content: action.content
        });
      }
    }
    if (plan.finalReply) {
      await memoryService.saveConversationMessage({
        ownerId: user.id,
        agentId: agent.id,
        namespace: agent.namespace,
        role: 'assistant',
        content: plan.finalReply,
        metadata: { source: 'codex_test', actionTypes }
      });
    }
  } catch (error) {
    console.log(JSON.stringify({
      text,
      error: error instanceof Error ? error.message : String(error)
    }));
  }
}

await prisma.$disconnect();
process.exit(0);
