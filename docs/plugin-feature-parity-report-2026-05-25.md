# Plugin Feature Parity Report - 2026-05-25

Audit source: `origin/main` after `git fetch origin main`.

Scope:
- Plugin packages: `plugins/claude-code/`, `plugins/codex/`, `plugins/cursor/`, `plugins/opencode/`, `plugins/pi/`
- Delegated Go runtimes followed from package hook commands: `plugins/sigil/internal/agents/claudecode/`, `plugins/sigil/internal/agents/codex/`, `plugins/sigil/internal/agents/cursor/`

GitHub issue creation status: not created in this automation run. The environment only exposes read-only `gh` access and no issue-creation MCP/tool, so this file is the issue-ready body.

## Plugin Feature Parity Report

### Parity Matrix

| Category | Feature | Claude Code | Codex | Cursor | OpenCode | Pi |
|----------|---------|:-----------:|:-----:|:------:|:--------:|:--:|
| Host integration | Host-required plugin package/manifest | ✅ | ✅ | ✅ | ✅ | ✅ |
| Host integration | Runtime hook/API lifecycle integration | ✅ | ✅ | ✅ | ✅ | ✅ |
| Config | Shared `~/.config/sigil/config.env` / canonical `SIGIL_*` support | ✅ | ✅ | ✅ | ❌ | ✅ |
| Config | Auth modes `none`, `bearer`, `tenant`, `basic` | ❌ | ❌ | ❌ | ✅ | ✅ |
| Config | Endpoint normalization / export-path append | ✅ | ✅ | ✅ | ❌ | ✅ |
| Config | Debug logging option | ✅ | ✅ | ✅ | ❌ | ✅ |
| Identity/tags | User identity propagation or override | ✅ | ✅ | ✅ | ❌ | ❌ |
| Identity/tags | Custom `SIGIL_TAGS` | ✅ | ✅ | ✅ | ❌ | ❌ |
| Identity/tags | Built-in context tags beyond model/session | ✅ | ✅ | ✅ | ❌ | ❌ |
| OTel | Plugin-owned OTel provider/export setup | ✅ | ✅ | ✅ | ❌ | ✅ |
| OTel | Standard OTLP headers/service-name env support | ✅ | ✅ | ✅ | ➖ | ❌ |
| Generation | Completed-turn generation export | ✅ | ✅ | ✅ | ✅ | ✅ |
| Generation | Correct non-stream `SYNC` export | ✅ | ✅ | ✅ | ✅ | ➖ |
| Generation | Streaming generation + TTFT where host exposes chunks | ❌ | ➖ | ❌ | ➖ | ✅ |
| Content | Three capture modes: `metadata_only`, `no_tool_content`, `full` | ✅ | ✅ | ✅ | ❌ | ✅ |
| Content | `metadata_only` default posture | ✅ | ✅ | ✅ | ❌ | ✅ |
| Content | Secret redaction for captured content | ✅ | ✅ | ❌ | ❌ | ✅ |
| Content | Structured JSON sensitive-key redaction for tool JSON | ❌ | ✅ | ❌ | ❌ | ❌ |
| Content | User/system prompt redaction when content is captured | ✅ | ✅ | ❌ | ❌ | ✅ |
| Tools | Tool definitions in generation seed | ✅ | ✅ | ✅ | ✅ | ✅ |
| Tools | Tool calls/results represented in generation output | ✅ | ✅ | ✅ | ✅ | ✅ |
| Tools | Separate tool execution spans/records | ✅ | ✅ | ✅ | ❌ | ✅ |
| Tools | Real tool execution timing when host exposes timing | ❌ | ✅ | ✅ | ❌ | ✅ |
| Guards | Sigil guard evaluation where host exposes a pre-tool decision point | ✅ | ➖ | ➖ | ➖ | ✅ |
| Subagents | Parent generation linking / subagent lineage where host exposes it | ✅ | ✅ | ➖ | ➖ | ➖ |
| Usage | Basic token usage | ✅ | ✅ | ✅ | ✅ | ✅ |
| Usage | Reasoning token usage | ❌ | ✅ | ➖ | ✅ | ➖ |
| Usage | Cache read/write token usage | ✅ | ➖ | ✅ | ✅ | ✅ |
| Usage | Cost metadata when host exposes it | ➖ | ➖ | ➖ | ✅ | ✅ |
| Result fields | Provider response ID mapping when host exposes it | ❌ | ➖ | ➖ | ➖ | ✅ |
| Result fields | Response model and stop/finish reason | ✅ | ✅ | ✅ | ✅ | ✅ |
| Errors | LLM call error classification when host exposes it | ❌ | ➖ | ✅ | ✅ | ✅ |
| Lifecycle | Stranded-turn recovery appropriate to host architecture | ✅ | ➖ | ✅ | ➖ | ➖ |
| Tests | Runtime/source tests for mapper and lifecycle behavior | ✅ | ✅ | ✅ | ❌ | ✅ |

Legend: ✅ implemented, ❌ missing/actionable, ➖ not relevant because of host-agent data availability or architecture.

### Gaps By Plugin

#### Claude Code

- [ ] **Non-basic auth modes** - Go hook runtime requires `SIGIL_AUTH_TENANT_ID` + `SIGIL_AUTH_TOKEN`; OpenCode and Pi support `none`, `bearer`, `tenant`, and `basic`. Reference: `plugins/sigil/internal/agents/claudecode/hook.go`, `plugins/opencode/src/client.ts`, `plugins/pi/src/config.ts`
- [ ] **Streaming mode and TTFT** - Claude Code coalesces streamed transcript fragments but exports `SYNC` generations only; Pi uses `startStreamingGeneration` and records first-token time from streaming updates. Reference: `plugins/sigil/internal/agents/claudecode/mapper/mapper.go`, `plugins/pi/src/index.ts`
- [ ] **Structured JSON sensitive-key redaction** - Codex decodes tool JSON, redacts sensitive keys, and re-marshals valid JSON; Claude Code redacts/truncates tool input as text. Reference: `plugins/sigil/internal/agents/codex/mapper/mapper.go`, `plugins/sigil/internal/agents/claudecode/mapper/mapper.go`
- [ ] **Reasoning token usage** - Codex and OpenCode map reasoning tokens when available; Claude Code usage mapping does not expose reasoning token counts. Reference: `plugins/sigil/internal/agents/claudecode/transcript/transcript.go`, `plugins/sigil/internal/agents/codex/mapper/mapper.go`, `plugins/opencode/src/mappers.ts`
- [ ] **Provider response ID mapping** - Claude Code transcript assistant messages include provider/message IDs, but the mapper does not set `response_id`; Pi maps `responseId`. Reference: `plugins/sigil/internal/agents/claudecode/transcript/transcript.go`, `plugins/sigil/internal/agents/claudecode/mapper/mapper.go`, `plugins/pi/src/mappers.ts`
- [ ] **Real tool execution timing** - Claude Code emits tool spans with parent-generation completion timestamps rather than a real tool window; Codex, Cursor, and Pi use host timing or duration fields. Reference: `plugins/sigil/internal/agents/claudecode/hook.go`, `plugins/sigil/internal/agents/cursor/hook/emit.go`, `plugins/pi/src/index.ts`
- [ ] **LLM call error classification** - Cursor, OpenCode, and Pi map failed assistant turns to call errors; Claude Code drops zero-token recovery rows and does not classify LLM call failures. Reference: `plugins/sigil/internal/agents/claudecode/mapper/mapper.go`, `plugins/sigil/internal/agents/cursor/mapper/mapper.go`, `plugins/opencode/src/mappers.ts`, `plugins/pi/src/index.ts`

#### Codex

- [ ] **Non-basic auth modes** - Codex uses the shared Go hook auth contract requiring Grafana Cloud-style basic credentials, while OpenCode and Pi support `none`, `bearer`, `tenant`, and `basic`. Reference: `plugins/sigil/internal/agents/codex/hook/handlers.go`, `plugins/opencode/src/client.ts`, `plugins/pi/src/config.ts`

#### Cursor

- [ ] **Non-basic auth modes** - Cursor uses the shared Go hook auth contract requiring Grafana Cloud-style basic credentials, while OpenCode and Pi support `none`, `bearer`, `tenant`, and `basic`. Reference: `plugins/sigil/internal/agents/cursor/config/config.go`, `plugins/opencode/src/client.ts`, `plugins/pi/src/config.ts`
- [ ] **Streaming mode and TTFT** - Cursor receives streamed `afterAgentResponse` updates but exports merged `SYNC` generations; Pi exports `STREAM` and records TTFT. Reference: `plugins/sigil/internal/agents/cursor/hook/afterresponse.go`, `plugins/sigil/internal/agents/cursor/mapper/mapper.go`, `plugins/pi/src/index.ts`
- [ ] **Secret redaction in full content mode** - Cursor README promises automatic redaction, but the runtime does not wire the SDK sanitizer or the shared redactor; Claude Code, Codex, and Pi redact captured content. Reference: `plugins/cursor/README.md`, `plugins/sigil/internal/agents/cursor/mapper/mapper.go`, `plugins/pi/src/client.ts`
- [ ] **Structured JSON sensitive-key redaction** - Codex redacts sensitive keys inside tool JSON while preserving valid JSON; Cursor does not redact captured full-mode tool payloads. Reference: `plugins/sigil/internal/agents/codex/mapper/mapper.go`, `plugins/sigil/internal/agents/cursor/mapper/mapper.go`
- [ ] **User/system prompt redaction** - Claude Code, Codex, and Pi apply redaction to captured user/prompt content; Cursor full-mode content passes through verbatim. Reference: `plugins/sigil/internal/agents/cursor/mapper/mapper.go`, `plugins/pi/src/client.ts`

#### OpenCode

- [ ] **Shared Sigil config/env support** - OpenCode only reads `~/.config/opencode/opencode-sigil.json`; the other plugins can share `~/.config/sigil/config.env` and canonical `SIGIL_*` settings. Reference: `plugins/opencode/src/config.ts`, `plugins/pi/src/sigilDotenv.ts`, `plugins/cursor/README.md`
- [ ] **Endpoint normalization** - OpenCode warns when the endpoint lacks `/api/v1/generations:export` and still passes it through; Go plugins and Pi append/normalize the export path. Reference: `plugins/opencode/src/client.ts`, `plugins/pi/src/client.ts`
- [ ] **Debug logging option** - OpenCode has setup warnings but no `SIGIL_DEBUG`/config debug mode comparable to the other plugins. Reference: `plugins/opencode/src/hooks.ts`, `plugins/pi/src/client.ts`, `plugins/cursor/README.md`
- [ ] **User identity propagation or override** - Claude Code and Cursor resolve/propagate user identity and Codex documents `SIGIL_USER_ID`; OpenCode does not propagate user identity. Reference: `plugins/opencode/src/hooks.ts`, `plugins/sigil/internal/agents/claudecode/userid.go`, `plugins/sigil/internal/agents/cursor/mapper/mapper.go`
- [ ] **Custom and built-in tags** - OpenCode does not pass `SIGIL_TAGS` or built-in context tags such as `cwd`, `git.branch`, or agent role. Reference: `plugins/opencode/src/hooks.ts`, `plugins/sigil/internal/agents/cursor/tags/tags.go`
- [ ] **OTel provider/export setup** - OpenCode creates a Sigil client without tracer/meter providers, so SDK spans and metrics are no-op; Claude Code, Codex, Cursor, and Pi configure OTel when OTLP is set. Reference: `plugins/opencode/src/client.ts`, `plugins/pi/src/telemetry.ts`, `plugins/sigil/internal/otel/otel.go`
- [ ] **Three capture modes and metadata-only default** - OpenCode exposes a boolean `contentCapture` and code defaults to captured content (`?? true`), while the other plugins support `metadata_only`, `no_tool_content`, and `full` with metadata-only default. Reference: `plugins/opencode/src/hooks.ts`, `plugins/opencode/README.md`, `plugins/pi/src/config.ts`
- [ ] **Secret redaction parity** - OpenCode redacts assistant and tool output but does not redact user input or system prompts, and lacks structured JSON sensitive-key redaction. Reference: `plugins/opencode/src/mappers.ts`, `plugins/opencode/src/redact.ts`, `plugins/sigil/internal/agents/codex/mapper/mapper.go`
- [ ] **Separate tool execution spans** - OpenCode embeds tool calls/results in generation output but never calls `startToolExecution`; Claude Code, Codex, Cursor, and Pi emit separate tool records/spans. Reference: `plugins/opencode/src/hooks.ts`, `plugins/pi/src/index.ts`, `plugins/sigil/internal/agents/cursor/hook/emit.go`
- [ ] **Runtime/lifecycle test coverage** - OpenCode tests mappers and redaction only; Pi and the Go runtimes cover lifecycle, config/client, telemetry, guards, and tool span behavior. Reference: `plugins/opencode/src/mappers.test.ts`, `plugins/opencode/src/redact.test.ts`, `plugins/pi/src/index.test.ts`

#### Pi

- [ ] **User identity propagation or override** - Claude Code/Cursor/Codex expose user identity override or host-derived user identity; Pi does not pass user ID into the JS SDK client or generation context. Reference: `plugins/pi/src/client.ts`, `plugins/sigil/internal/agents/cursor/mapper/mapper.go`, `plugins/sigil/internal/agents/claudecode/userid.go`
- [ ] **Custom `SIGIL_TAGS` and broad built-in context tags** - Pi parses shared dotenv keys but does not resolve `SIGIL_TAGS`; it only emits `git.branch` in `full` mode and omits broader context tags such as `cwd`. Reference: `plugins/pi/src/config.ts`, `plugins/pi/src/index.ts`, `plugins/sigil/internal/agents/cursor/tags/tags.go`
- [ ] **Standard OTLP headers/service-name env support** - `OTEL_EXPORTER_OTLP_HEADERS` and `OTEL_SERVICE_NAME` are allowed by the dotenv loader, but `resolveOtlp` does not consume those standard env keys and telemetry hardcodes `service.name=sigil-pi`. Reference: `plugins/pi/src/sigilDotenv.ts`, `plugins/pi/src/config.ts`, `plugins/pi/src/telemetry.ts`
- [ ] **Structured JSON sensitive-key redaction** - Codex redacts sensitive keys inside tool JSON while preserving valid JSON; Pi relies on the SDK sanitizer and does not add structured JSON key redaction for tool arguments/results. Reference: `plugins/sigil/internal/agents/codex/mapper/mapper.go`, `plugins/pi/src/client.ts`, `plugins/pi/src/mappers.ts`

### Prioritized Instrumentation Opportunities

1. **OpenCode OTel + capture/privacy parity**
   - Paths: `plugins/opencode/src/client.ts`, `plugins/opencode/src/config.ts`, `plugins/opencode/src/hooks.ts`, `plugins/opencode/src/mappers.ts`
   - Why: OpenCode users lose SDK spans/metrics and default to content capture in code despite README claiming metadata-only.
   - Diff proposal: add OTel provider setup mirroring Pi; replace boolean `contentCapture` with tri-mode parsing; default to `metadata_only`; append/normalize the export path; wire SDK sanitizer/redaction for user/system content.
   - Test plan: add config/client/hook tests for mode defaults, OTel wiring, endpoint normalization, and redaction on user/system/tool JSON.
   - Risk: config migration from boolean to string should keep boolean compatibility.

2. **Cursor full-mode redaction**
   - Paths: `plugins/sigil/internal/agents/cursor/hook/emit.go`, `plugins/sigil/internal/agents/cursor/mapper/mapper.go`, `plugins/sigil/internal/agents/cursor/hook/posttooluse.go`
   - Why: Cursor README promises automatic secret redaction but captured full-mode content is passed through verbatim.
   - Diff proposal: wire the shared secret sanitizer and/or shared redactor at generation and tool-span boundaries; add structured JSON redaction for tool inputs/results.
   - Test plan: extend mapper/emit tests with Grafana token, bearer token, and sensitive JSON-key payloads.
   - Risk: redaction changes full-mode payload contents; preserve structural tool parts.

3. **Go hook auth-mode parity**
   - Paths: `plugins/sigil/internal/agents/claudecode/hook.go`, `plugins/sigil/internal/agents/codex/hook/handlers.go`, `plugins/sigil/internal/agents/cursor/config/config.go`, shared config/client helpers
   - Why: Claude Code, Codex, and Cursor cannot use local `none`, bearer, or tenant auth while OpenCode/Pi can.
   - Diff proposal: add shared auth resolver for `SIGIL_AUTH_MODE` plus bearer/tenant/basic fields while preserving existing Grafana Cloud basic defaults.
   - Test plan: add hook client construction tests for basic default, none, bearer, and tenant modes.
   - Risk: must not break existing `SIGIL_AUTH_TENANT_ID` + `SIGIL_AUTH_TOKEN` deployments.

4. **Tags and identity parity for TypeScript plugins**
   - Paths: `plugins/opencode/src/config.ts`, `plugins/opencode/src/hooks.ts`, `plugins/pi/src/config.ts`, `plugins/pi/src/index.ts`, `plugins/pi/src/client.ts`
   - Why: Go plugins support `SIGIL_TAGS` and user identity, while OpenCode/Pi do not expose equivalent filtering/correlation.
   - Diff proposal: parse `SIGIL_TAGS`, support `SIGIL_USER_ID`, and attach per-generation tags/user identity through SDK-supported APIs; add safe built-ins (`cwd`, `git.branch` where privacy mode permits).
   - Test plan: add config and mapper/hook tests for tag precedence and user ID propagation.
   - Risk: confirm JS SDK support for user identity before adding adapter-level fields.

5. **Provider/result field completeness**
   - Paths: `plugins/sigil/internal/agents/claudecode/transcript/transcript.go`, `plugins/sigil/internal/agents/claudecode/mapper/mapper.go`
   - Why: Claude Code has transcript data that can improve correlation and usage fidelity.
   - Diff proposal: map assistant `message.id` to `response_id`; parse/map reasoning tokens when present; set call errors for failed assistant turns.
   - Test plan: add transcript fixtures covering response IDs, reasoning-token usage, and error turns.
   - Risk: ensure older transcript schemas continue to parse with zero values.

6. **Streaming/TTFT where host chunks exist**
   - Paths: `plugins/sigil/internal/agents/cursor/hook/afterresponse.go`, `plugins/sigil/internal/agents/cursor/mapper/mapper.go`, `plugins/sigil/internal/agents/claudecode/mapper/mapper.go`
   - Why: Pi users get stream mode and TTFT; Cursor and Claude Code see streamed/chunked host data but export sync generations.
   - Diff proposal: record first response chunk timestamp and use streaming generation mode where the host data provides a real first-token boundary.
   - Test plan: add chunked transcript/after-response fixtures and assert `STREAM` mode plus first-token timestamp.
   - Risk: avoid marking batch-only transcript replay as streaming unless timestamps are trustworthy.

### Already Tracked

No open `enhancement` issues were returned by:

```sh
gh issue list --repo grafana/sigil-sdk --state open --label enhancement --limit 200
```

Automation memory also showed previous draft audits where no GitHub issue was created.

### Context

Created by the automated plugin feature parity audit. The report intentionally marks architecture and host-limit differences as ➖ rather than gaps: hook subprocess state vs in-process state, plugin packaging systems, missing host token/cost/subagent fields, and host lifecycles without session-end equivalents.
