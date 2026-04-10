import { AsyncLocalStorage } from 'node:async_hooks';
import type { ContentCaptureMode } from './types.js';

type SigilContextValues = {
  conversationId?: string;
  conversationTitle?: string;
  userId?: string;
  agentName?: string;
  agentVersion?: string;
  contentCaptureMode?: ContentCaptureMode;
};

const storage = new AsyncLocalStorage<SigilContextValues>();

export function withConversationId<T>(conversationId: string, callback: () => T): T {
  return runWithContext({ conversationId }, callback);
}

export function withConversationTitle<T>(conversationTitle: string, callback: () => T): T {
  return runWithContext({ conversationTitle }, callback);
}

export function withUserId<T>(userId: string, callback: () => T): T {
  return runWithContext({ userId }, callback);
}

export function withAgentName<T>(agentName: string, callback: () => T): T {
  return runWithContext({ agentName }, callback);
}

export function withAgentVersion<T>(agentVersion: string, callback: () => T): T {
  return runWithContext({ agentVersion }, callback);
}

export function conversationIdFromContext(): string | undefined {
  return normalizedString(storage.getStore()?.conversationId);
}

export function conversationTitleFromContext(): string | undefined {
  return normalizedString(storage.getStore()?.conversationTitle);
}

export function userIdFromContext(): string | undefined {
  return normalizedString(storage.getStore()?.userId);
}

export function agentNameFromContext(): string | undefined {
  return normalizedString(storage.getStore()?.agentName);
}

export function agentVersionFromContext(): string | undefined {
  return normalizedString(storage.getStore()?.agentVersion);
}

export function withContentCaptureMode<T>(mode: ContentCaptureMode, callback: () => T): T {
  const currentValues = storage.getStore() ?? {};
  const mergedValues: SigilContextValues = { ...currentValues, contentCaptureMode: mode };
  return storage.run(mergedValues, callback);
}

export function contentCaptureModeFromContext(): { mode: ContentCaptureMode; set: boolean } {
  const value = storage.getStore()?.contentCaptureMode;
  if (value === undefined) {
    return { mode: 'default', set: false };
  }
  return { mode: value, set: true };
}

function runWithContext<T>(nextValues: Omit<SigilContextValues, 'contentCaptureMode'>, callback: () => T): T {
  const currentValues = storage.getStore() ?? {};
  const mergedValues: SigilContextValues = { ...currentValues };

  for (const [key, value] of Object.entries(nextValues)) {
    const normalized = normalizedString(value as string | undefined);
    if (normalized === undefined) {
      delete mergedValues[key as keyof typeof nextValues];
      continue;
    }
    mergedValues[key as keyof typeof nextValues] = normalized;
  }

  return storage.run(mergedValues, callback);
}

function normalizedString(value: string | undefined): string | undefined {
  if (value === undefined) {
    return undefined;
  }
  const trimmed = value.trim();
  return trimmed.length > 0 ? trimmed : undefined;
}
