import { env } from './config/env.js';
import { connectMongo } from './db/mongo.js';
import { logger } from './libs/logger.js';
import { createApp } from './app.js';

await connectMongo();

const app = createApp();

app.listen(env.PORT, () => {
  logger.info({ port: env.PORT }, `${env.APP_NAME} API running`);
});
