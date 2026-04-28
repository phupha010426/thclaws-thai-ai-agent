import { auditService } from '../audit/audit.service.js';
import { SlipRecord, UserAccountIdentity } from '../memory/memory.model.js';
import type { ImageUnderstandingResult } from '../agents/image-understanding.service.js';

export class SlipService {
  async createPendingSlip(input: {
    ownerId: string;
    agentId: string;
    namespace: string;
    storedImageId: unknown;
  }) {
    const slip = await SlipRecord.create({
      ownerId: input.ownerId,
      agentId: input.agentId,
      namespace: input.namespace,
      storedImageId: input.storedImageId,
      status: 'PENDING_OCR',
      direction: 'UNKNOWN'
    });

    await auditService.log({
      actorId: input.ownerId,
      action: 'slip.image_received',
      resource: 'slip_records',
      metadata: { slipId: String(slip._id), namespace: input.namespace }
    });

    return slip;
  }

  async applyVisionResult(input: {
    ownerId: string;
    slipId: unknown;
    vision: ImageUnderstandingResult;
  }) {
    const direction = input.vision.directionHint === 'UNKNOWN'
      ? await this.classifyFromAccounts({
        ownerId: input.ownerId,
        fromAccountMasked: input.vision.fromAccountMasked,
        toAccountMasked: input.vision.toAccountMasked
      })
      : input.vision.directionHint ?? 'UNKNOWN';

    await SlipRecord.updateOne(
      { _id: input.slipId, ownerId: input.ownerId },
      {
        $set: {
          status: input.vision.isSlip ? 'NEEDS_CONFIRMATION' : 'PARSED',
          direction,
          amount: input.vision.amount ?? null,
          fromAccountMasked: input.vision.fromAccountMasked ?? null,
          toAccountMasked: input.vision.toAccountMasked ?? null,
          rawText: input.vision.finalReply,
          parserVersion: 'smlgateway-vision-v1'
        }
      }
    );

    await auditService.log({
      actorId: input.ownerId,
      action: 'slip.vision_parsed',
      resource: 'slip_records',
      metadata: {
        slipId: String(input.slipId),
        isSlip: input.vision.isSlip,
        direction,
        confidence: input.vision.confidence
      }
    });

    return direction;
  }

  async classifyFromAccounts(input: {
    ownerId: string;
    fromAccountMasked?: string | null;
    toAccountMasked?: string | null;
  }) {
    const ownAccounts = await UserAccountIdentity.find({ ownerId: input.ownerId, isOwnerAccount: true }).lean();
    const ownSet = new Set(ownAccounts.map((account) => account.accountNoMasked));
    const fromIsOwn = Boolean(input.fromAccountMasked && ownSet.has(input.fromAccountMasked));
    const toIsOwn = Boolean(input.toAccountMasked && ownSet.has(input.toAccountMasked));
    if (fromIsOwn && toIsOwn) return 'TRANSFER';
    if (toIsOwn) return 'INCOME';
    if (fromIsOwn) return 'EXPENSE';
    return 'UNKNOWN';
  }
}

export const slipService = new SlipService();
