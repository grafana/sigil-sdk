# Getting Started — Python

Makes an OpenAI chat completion and records the generation to Sigil.

## Setup

```bash
cd examples/getting-started/python
# Set OPENAI_API_KEY, GRAFANA_INSTANCE_ID, GRAFANA_CLOUD_TOKEN, SIGIL_ENDPOINT
# See the SDK README for where to find each value.
```

```bash
pip install -r requirements.txt
```

## Run

```bash
python main.py
```

You should see the LLM response printed, followed by `Done`. Open the AI Observability plugin in your Grafana Cloud stack to see the recorded generation.
