# Getting Started — TypeScript

Makes an OpenAI chat completion and records the generation to Sigil.

## Setup

```bash
cd examples/getting-started/typescript
cp .env.example .env
# Fill in your credentials (see ../README.md for where to find each value)
```

```bash
npm install
```

> The `@grafana/sigil-sdk-js` package is installed from the local monorepo via a `file:` reference. If you're working outside the monorepo, replace it with the published package once available.

## Run

```bash
npx tsx main.ts
```

You should see the LLM response printed, followed by `Done`. Open the AI Observability plugin in your Grafana Cloud stack to see the recorded generation.
