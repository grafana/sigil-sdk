import type { Plugin } from "@opencode-ai/plugin";
import { loadSigilConfig } from "./config.js";
import { createSigilHooks } from "./hooks.js";

export const SigilPlugin: Plugin = async ({ client }) => {
  const config = await loadSigilConfig();
  if (!config.enabled) return {};

  const hooks = await createSigilHooks(config, client);
  if (!hooks) return {};

  return {
    "chat.message": async (input, output) => {
      hooks.chatMessage(input, output);
    },
    "experimental.chat.system.transform": async (input, output) => {
      hooks.systemTransform(input, output);
    },
    event: async ({ event }) => {
      await hooks.event({
        event: event as { type: string; properties: unknown },
      });
    },
  };
};
