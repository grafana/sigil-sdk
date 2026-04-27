"""Minimal AI Observability getting-started example — Python + OpenAI."""

import os

from dotenv import load_dotenv
from openai import OpenAI
from sigil_sdk import (
    AuthConfig,
    Client,
    ClientConfig,
    GenerationExportConfig,
    GenerationStart,
    ModelRef,
    TokenUsage,
    assistant_text_message,
    user_text_message,
)

load_dotenv()

openai_client = OpenAI()
model = "gpt-4.1-mini"

sigil = Client(
    ClientConfig(
        generation_export=GenerationExportConfig(
            protocol="http",
            endpoint=os.environ["SIGIL_ENDPOINT"],
            auth=AuthConfig(
                mode="basic",
                tenant_id=os.environ["GRAFANA_INSTANCE_ID"],
                basic_password=os.environ["GRAFANA_CLOUD_TOKEN"],
            ),
        ),
    )
)

prompt = "Explain what LLM observability is in two sentences."

completion = openai_client.chat.completions.create(
    model=model,
    messages=[
        {"role": "system", "content": "You are a helpful assistant."},
        {"role": "user", "content": prompt},
    ],
)

response_text = completion.choices[0].message.content
usage = completion.usage
print(f"Response: {response_text}\n")

with sigil.start_generation(
    GenerationStart(
        conversation_id="getting-started-python",
        agent_name="getting-started",
        agent_version="1.0.0",
        model=ModelRef(provider="openai", name=model),
    )
) as rec:
    rec.set_result(
        input=[user_text_message(prompt)],
        output=[assistant_text_message(response_text or "")],
        response_id=completion.id,
        response_model=completion.model,
        stop_reason=completion.choices[0].finish_reason or "",
        usage=TokenUsage(
            input_tokens=usage.prompt_tokens if usage else 0,
            output_tokens=usage.completion_tokens if usage else 0,
        ),
    )
    if rec.err() is not None:
        print("SDK error:", rec.err())

sigil.shutdown()
print("Done — check the AI Observability plugin in your Grafana Cloud stack.")
