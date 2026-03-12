import { AsyncLocalStorage } from 'node:async_hooks';

type SigilContextValues = {
  conversationId?: string;
  conversationTitle?: string;
  userId?: string;
  agentName?: string;
  agentVersion?: string;
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

function runWithContext<T>(nextValues: SigilContextValues, callback: () => T): T {
  const currentValues = storage.getStore() ?? {};
  const mergedValues: SigilContextValues = { ...currentValues };

  for (const [key, value] of Object.entries(nextValues)) {
    const normalized = normalizedString(value);
    if (normalized === undefined) {
      delete mergedValues[key as keyof SigilContextValues];
      continue;
    }
    mergedValues[key as keyof SigilContextValues] = normalized;
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
