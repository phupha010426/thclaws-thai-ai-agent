import { Client } from 'minio';
import { env } from '../../config/env.js';
import { logger } from '../../libs/logger.js';

export class ObjectStorageService {
  private readonly client = new Client({
    endPoint: env.MINIO_ENDPOINT,
    port: env.MINIO_PORT,
    useSSL: env.MINIO_USE_SSL,
    accessKey: env.MINIO_ACCESS_KEY,
    secretKey: env.MINIO_SECRET_KEY
  });

  async ensureBucket() {
    const exists = await this.client.bucketExists(env.MINIO_BUCKET);
    if (!exists) {
      await this.client.makeBucket(env.MINIO_BUCKET);
      logger.info({ bucket: env.MINIO_BUCKET }, 'created object storage bucket');
    }
  }

  async putObject(input: { objectName: string; buffer: Buffer; contentType: string }) {
    await this.ensureBucket();
    await this.client.putObject(env.MINIO_BUCKET, input.objectName, input.buffer, input.buffer.length, {
      'Content-Type': input.contentType
    });
    return {
      bucket: env.MINIO_BUCKET,
      objectName: input.objectName
    };
  }
}

export const objectStorageService = new ObjectStorageService();
