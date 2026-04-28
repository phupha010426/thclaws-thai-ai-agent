import { nanoid } from 'nanoid';
import { objectStorageService } from '../storage/object-storage.service.js';
import { StoredImage } from './memory.model.js';
import { logger } from '../../libs/logger.js';

export class ImageService {
  async storeLineImage(input: {
    ownerId: string;
    agentId: string;
    namespace: string;
    lineMessageId: string;
    buffer: Buffer;
    contentType: string;
  }) {
    const ext = contentTypeToExtension(input.contentType);
    const objectName = `${input.ownerId}/${new Date().toISOString().slice(0, 10)}/${nanoid()}${ext}`;
    const stored = await objectStorageService.putObject({
      objectName,
      buffer: input.buffer,
      contentType: input.contentType
    });
    logger.info({
      ownerId: input.ownerId,
      namespace: input.namespace,
      bucket: stored.bucket,
      objectName: stored.objectName,
      size: input.buffer.length,
      contentType: input.contentType
    }, 'line image stored');

    return StoredImage.create({
      ownerId: input.ownerId,
      agentId: input.agentId,
      namespace: input.namespace,
      lineMessageId: input.lineMessageId,
      bucket: stored.bucket,
      objectName: stored.objectName,
      contentType: input.contentType,
      size: input.buffer.length,
      source: 'line'
    });
  }
}

function contentTypeToExtension(contentType: string) {
  if (contentType.includes('png')) return '.png';
  if (contentType.includes('webp')) return '.webp';
  return '.jpg';
}

export const imageService = new ImageService();
