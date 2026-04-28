export type LineTextMessageEvent = {
  type: 'message';
  replyToken?: string;
  source?: {
    userId?: string;
    type?: string;
  };
  message: {
    id?: string;
    type: 'text';
    text: string;
  };
};

export type LineImageMessageEvent = {
  type: 'message';
  replyToken?: string;
  source?: {
    userId?: string;
    type?: string;
  };
  message: {
    id: string;
    type: 'image';
  };
};

export function isLineTextMessageEvent(event: unknown): event is LineTextMessageEvent {
  if (!event || typeof event !== 'object') return false;
  const candidate = event as LineTextMessageEvent;
  return candidate.type === 'message' && candidate.message?.type === 'text' && typeof candidate.message.text === 'string';
}

export function isLineImageMessageEvent(event: unknown): event is LineImageMessageEvent {
  if (!event || typeof event !== 'object') return false;
  const candidate = event as LineImageMessageEvent;
  return candidate.type === 'message' && candidate.message?.type === 'image' && typeof candidate.message.id === 'string';
}
