# Python LangGraph Experiments SDK — First Iteration Plan

Status: implemented (first iteration), 2026-05-28
Owner: sigil-sdk
Related server design: `sigil/docs/design-docs/2026-05-05-sdk-experiment-runner.md`,
`sigil/docs/references/offline-experiment-export.md`

## Goal

Let users run a LangGraph agent over a problem dataset (or A/B two agent versions
on the same task) as a **Sigil offline-evaluation experiment**, grade locally,
and compare runs in Sigil — with minimal code changes, CI-friendly ergonomics,
and a pain-free coding-agent experience. This builds on the existing LangGraph
generation instrumentation rather than replacing it.

## Design principles applied

1. **Build on existing instrumentation.** The runner reuses the LangGraph
   callback handler; the only experiment-specific addition is carrying the
   `run_id` in generation tags/metadata and on exported scores.
2. **Minimal code changes.** Wrapping a graph in an experiment is a few lines:
   create a runner, pass `run.langgraph_config()` into `graph.invoke(...)`.
3. **CI-friendly.** Stable, caller-provided `run_id`; deterministic score IDs for
   idempotent retries; config via `SIGIL_*` env vars; a copy-paste example.
4. **Generation-first, publish-continuously.** Create → run+grade per item →
   finalize; no separate "upload" step. The default publishes every conversation
   and score live (users can delete experiments).

## Architecture

Two layers:

### Layer 1 — Core SDK control/score APIs (`sigil-sdk`, language-agnostic)

New typed models in `sigil_sdk.models`: `ScoreValue`, `ScoreSource`, `ScoreItem`,
`ExportScoreResult`, `ExportScoresResponse`, `ExperimentStatus`, `ExperimentSource`,
`ExperimentEvaluator`, `CreateExperimentRequest`, `UpdateExperimentRequest`,
`Experiment`, `ExperimentReportSummary`, `ExperimentReport`.

New transport module `sigil_sdk.experiments` (HTTP, mirrors the conversation
rating + hooks transport) with retry + exponential backoff on timeouts/429/5xx
and status→error mapping (400→`ValidationError`, 404→`NotFoundError`,
409→`ConflictError`).

New `Client` methods: `create_experiment`, `get_experiment`, `update_experiment`,
`complete_experiment` (finalize convenience), `cancel_experiment`, `export_scores`,
`list_experiment_scores`, `get_experiment_report`, and `experiment_url` (best-effort
deep link; override via `SIGIL_EXPERIMENT_URL_TEMPLATE`).

These map to the Sigil endpoints:
`POST/GET/PATCH /api/v1/eval/experiments[/{run_id}]`,
`POST /api/v1/eval/experiments/{run_id}:cancel`,
`POST /api/v1/scores:export`,
`GET /api/v1/eval/experiments/{run_id}/{scores,report}`.

### Layer 2 — LangGraph runner (`sigil-sdk-langgraph`)

`sigil_sdk_langgraph.experiment` provides:

- `experiment(...)` — context manager. Creates an `external` run, exposes an
  `ExperimentRun`, and finalizes on exit: `succeeded` (clean), `failed`
  (exception), `canceled` (`KeyboardInterrupt`).
- `ExperimentRun` — `langgraph_config(config)` returns an invocation config with a
  handler pre-tagged with `experiment.run_id` (tag) and `experiment_run_id`
  (metadata); `add_scores(...)` normalizes + publishes scores; `produced_generation_ids`
  exposes ids captured during the last invoke (so scores attach automatically);
  `publish()`/`finalize()` for manual control; `report()`/`url`.
- `ExperimentRunner` — loops a dataset, calling a user `target(item, run)` and
  user `scorer(item, result)` callables, exporting scores per item.
- Dataclasses `DatasetItem`, `TargetResult`, `ScoreOutput`; `stable_id` helper.

Generation-id capture uses a thin handler subclass that forwards the base
handler's tracked ids into a run-owned sink, so users don't plumb ids by hand.

## Usage flows

- **New run / dataset sweep:** `ExperimentRunner.run(items, target, scorers)`.
- **A/B testing (first class):** two runners with different `run_id`/`tags`/
  `candidate` over the same items + scorers; compare in the UI.
- **Ad-hoc single task:** `with experiment(...) as run:` and drive the loop.
- **Upload an old run:** documented in the skill — replay stored transcripts
  conversation-by-conversation (`start_generation`→`flush`→`export_scores`→
  `complete_experiment`). No first-class importer in this cut, by design.

## Grading

User-supplied scorers only. LLM-as-judge is a user scorer that records its own
generation (shown in the skill). No built-in judge in this iteration.

## Score metadata contract

Each score carries `dataset_id`, `dataset_version`, `item_id`, `task_id`,
`task_category`, `trial_id`, and `candidate`, sourced from the run spec and
`DatasetItem.metadata`. These are the keys the Sigil report/plugin group by.

## Edge cases handled

- **Crash mid-run:** the context manager finalizes `failed` with the error.
- **User interrupt:** `KeyboardInterrupt` cancels the run (`canceled`, CAS on the
  server so it never clobbers a concurrent terminal transition).
- **Transient API failures:** retries with exponential backoff on
  timeouts/429/5xx; deterministic `score_id`s make retried exports idempotent.

## Premium UX

- Terminal deep link printed on finish (`result.url` / `run.url`).
- Upload modes: `continuous` (default), `bulk`, `manual` (inspect-then-publish).

## Deliverables in this iteration

- Core SDK models + `experiments` transport + `Client` methods + tests
  (`python/tests/test_experiments_transport.py`).
- LangGraph `experiment` module + tests
  (`python-frameworks/langgraph/tests/test_experiment.py`).
- Coding-agent skill (`skills/sigil-langgraph-experiments/SKILL.md`).
- Runnable example (`examples/python-langgraph-experiment/`).
- Package versions bumped to `0.6.0`.

## Deferred follow-ups

- CLI / console entry point (`sigil-experiment`).
- Concurrency > 1 in the runner.
- Built-in LLM-as-judge scorer helper.
- First-class collection-run control helper + `wait_for_experiment`.
- First-class old-experiment importer + JSONL dataset loaders.
- Client-side gate helpers over report summaries/breakdowns.
- Parity ports (TypeScript, Go, Java, .NET).
