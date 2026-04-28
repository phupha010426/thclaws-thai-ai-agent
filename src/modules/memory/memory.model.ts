import mongoose, { Schema } from 'mongoose';

const MemoryItemSchema = new Schema(
  {
    ownerId: { type: String, required: true, index: true },
    agentId: { type: String, required: true, index: true },
    namespace: { type: String, required: true, index: true },
    type: { type: String, required: true },
    content: { type: String, required: true },
    sensitivityLevel: { type: String, enum: ['low', 'medium', 'high'], default: 'low' },
    confidence: { type: Number, default: 1 },
    source: { type: String, default: 'line' },
    expiresAt: { type: Date, default: null },
    metadata: { type: Schema.Types.Mixed, default: {} }
  },
  { collection: 'memory_items', timestamps: true }
);

const MemorySummarySchema = new Schema(
  {
    ownerId: { type: String, required: true, index: true },
    agentId: { type: String, required: true, index: true },
    namespace: { type: String, required: true, index: true },
    summary: { type: String, required: true },
    messageCount: { type: Number, default: 0 },
    fromDate: Date,
    toDate: Date
  },
  { collection: 'memory_summaries', timestamps: true }
);

const ConversationMessageSchema = new Schema(
  {
    ownerId: { type: String, required: true, index: true },
    agentId: { type: String, required: true, index: true },
    namespace: { type: String, required: true, index: true },
    role: { type: String, enum: ['user', 'assistant', 'system'], required: true },
    content: { type: String, required: true },
    compacted: { type: Boolean, default: false, index: true },
    metadata: { type: Schema.Types.Mixed, default: {} }
  },
  { collection: 'conversation_messages', timestamps: true }
);

const EventLogSchema = new Schema(
  {
    ownerId: { type: String, required: true, index: true },
    agentId: { type: String, default: null, index: true },
    namespace: { type: String, required: true, index: true },
    eventType: { type: String, required: true, index: true },
    source: { type: String, default: 'line' },
    content: { type: String, default: null },
    metadata: { type: Schema.Types.Mixed, default: {} }
  },
  { collection: 'event_logs', timestamps: true }
);

const PersonalWikiPageSchema = new Schema(
  {
    ownerId: { type: String, required: true, index: true },
    agentId: { type: String, required: true, index: true },
    namespace: { type: String, required: true, index: true },
    title: { type: String, required: true },
    content: { type: String, required: true },
    source: { type: String, default: 'thclaws' }
  },
  { collection: 'personal_wiki_pages', timestamps: true }
);

const ConversationSnapshotSchema = new Schema(
  {
    namespace: { type: String, required: true, index: true },
    lineUserId: { type: String, required: true, index: true },
    lastIntent: String,
    metadata: { type: Schema.Types.Mixed, default: {} }
  },
  { collection: 'conversation_snapshots', timestamps: true }
);

const AgentProfileSchema = new Schema(
  {
    agentId: { type: String, required: true, unique: true },
    namespace: { type: String, required: true, index: true },
    preferences: { type: Schema.Types.Mixed, default: {} }
  },
  { collection: 'agent_profiles', timestamps: true }
);

const AgentProgramSchema = new Schema(
  {
    ownerId: { type: String, required: true, index: true },
    agentId: { type: String, required: true, index: true },
    namespace: { type: String, required: true, index: true },
    name: { type: String, required: true },
    description: { type: String, required: true },
    language: { type: String, enum: ['typescript', 'jsonlogic', 'prompt'], default: 'typescript' },
    source: { type: String, required: true },
    status: { type: String, enum: ['DRAFT', 'READY', 'DISABLED'], default: 'DRAFT' },
    createdBy: { type: String, default: 'thclaws' },
    metadata: { type: Schema.Types.Mixed, default: {} }
  },
  { collection: 'agent_programs', timestamps: true }
);

const StoredImageSchema = new Schema(
  {
    ownerId: { type: String, required: true, index: true },
    agentId: { type: String, required: true, index: true },
    namespace: { type: String, required: true, index: true },
    lineMessageId: { type: String, required: true, index: true },
    bucket: { type: String, required: true },
    objectName: { type: String, required: true },
    contentType: { type: String, required: true },
    size: { type: Number, required: true },
    source: { type: String, default: 'line' }
  },
  { collection: 'stored_images', timestamps: true }
);

const UserAccountIdentitySchema = new Schema(
  {
    ownerId: { type: String, required: true, index: true },
    accountNoMasked: { type: String, required: true, index: true },
    accountName: { type: String, default: null },
    bankName: { type: String, default: null },
    isOwnerAccount: { type: Boolean, default: true }
  },
  { collection: 'user_account_identities', timestamps: true }
);

const FinancialTransactionSchema = new Schema(
  {
    ownerId: { type: String, required: true, index: true },
    agentId: { type: String, required: true, index: true },
    namespace: { type: String, required: true, index: true },
    pgTransactionId: { type: String, default: null, index: true },
    type: { type: String, enum: ['INCOME', 'EXPENSE', 'TRANSFER'], required: true },
    amount: { type: Number, required: true },
    currency: { type: String, default: 'THB' },
    category: { type: String, required: true },
    note: { type: String, default: null },
    source: { type: String, default: 'line' },
    confidence: { type: Number, default: 1 },
    happenedAt: { type: Date, default: Date.now }
  },
  { collection: 'financial_transactions', timestamps: true }
);

const SlipRecordSchema = new Schema(
  {
    ownerId: { type: String, required: true, index: true },
    agentId: { type: String, required: true, index: true },
    namespace: { type: String, required: true, index: true },
    storedImageId: { type: Schema.Types.ObjectId, required: true, index: true },
    status: { type: String, enum: ['PENDING_OCR', 'PARSED', 'NEEDS_CONFIRMATION'], default: 'PENDING_OCR' },
    direction: { type: String, enum: ['INCOME', 'EXPENSE', 'TRANSFER', 'UNKNOWN'], default: 'UNKNOWN' },
    amount: { type: Number, default: null },
    fromAccountMasked: { type: String, default: null },
    toAccountMasked: { type: String, default: null },
    rawText: { type: String, default: null },
    parserVersion: { type: String, default: 'mvp-rule-v1' }
  },
  { collection: 'slip_records', timestamps: true }
);

MemoryItemSchema.index({ namespace: 1, type: 1, updatedAt: -1 });
MemoryItemSchema.index({ content: 'text' });
ConversationMessageSchema.index({ namespace: 1, compacted: 1, createdAt: -1 });
EventLogSchema.index({ namespace: 1, eventType: 1, createdAt: -1 });
PersonalWikiPageSchema.index({ namespace: 1, title: 1 }, { unique: true });
AgentProgramSchema.index({ namespace: 1, name: 1 }, { unique: true });

export const MemoryItem = mongoose.model('MemoryItem', MemoryItemSchema);
export const MemorySummary = mongoose.model('MemorySummary', MemorySummarySchema);
export const ConversationMessage = mongoose.model('ConversationMessage', ConversationMessageSchema);
export const EventLog = mongoose.model('EventLog', EventLogSchema);
export const ConversationSnapshot = mongoose.model('ConversationSnapshot', ConversationSnapshotSchema);
export const AgentProfile = mongoose.model('AgentProfile', AgentProfileSchema);
export const AgentProgram = mongoose.model('AgentProgram', AgentProgramSchema);
export const PersonalWikiPage = mongoose.model('PersonalWikiPage', PersonalWikiPageSchema);
export const StoredImage = mongoose.model('StoredImage', StoredImageSchema);
export const UserAccountIdentity = mongoose.model('UserAccountIdentity', UserAccountIdentitySchema);
export const FinancialTransaction = mongoose.model('FinancialTransaction', FinancialTransactionSchema);
export const SlipRecord = mongoose.model('SlipRecord', SlipRecordSchema);
