/**
 * Minimal Sigil getting-started example — TypeScript + OpenAI.
 */

import "dotenv/config";
import OpenAI from "openai";
import { createSigilClient } from "@grafana/sigil-sdk-js";
import type { GenerationRecorder } from "@grafana/sigil-sdk-js";

const openai = new OpenAI();
const model = "gpt-4.1-mini";

const sigil = createSigilClient({
  generationExport: {
    protocol: "http",
    endpoint: process.env.SIGIL_ENDPOINT!,
    auth: {
      mode: "basic",
      tenantId: process.env.GRAFANA_INSTANCE_ID!,
      basicPassword: process.env.GRAFANA_CLOUD_TOKEN!,
    },
  },
});

const prompt = "Explain what LLM observability is in two sentences.";

const completion = await openai.chat.completions.create({
  model,
  messages: [
    { role: "system", content: "You are a helpful assistant." },
    { role: "user", content: prompt },
  ],
});

const responseText = completion.choices[0].message.content ?? "";
const usage = completion.usage;
console.log(`Response: ${responseText}\n`);

await sigil.startGeneration(
  {
    conversationId: "getting-started-typescript",
    agentName: "getting-started",
    agentVersion: "1.0.0",
    model: { provider: "openai", name: model },
  },
  (rec: GenerationRecorder) => {
    rec.setResult({
      input: [{ role: "user", content: prompt }],
      output: [{ role: "assistant", content: responseText }],
      responseId: completion.id,
      responseModel: completion.model,
      stopReason: completion.choices[0].finish_reason ?? "",
      usage: {
        inputTokens: usage?.prompt_tokens ?? 0,
        outputTokens: usage?.completion_tokens ?? 0,
      },
    });
  },
);

await sigil.shutdown();
console.log(
  "Done — check the AI Observability plugin in your Grafana Cloud stack.",
);
