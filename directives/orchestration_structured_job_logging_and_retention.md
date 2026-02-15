# Orchestration: Structured Job Logging and Retention

## Phase
Orchestration + execution in current session.

## Objective
Execute `directives/structured_job_logging_and_retention.md` with deterministic delivery: structured logs, per-job files, and periodic retention enforcement.

## Workstreams

## WS1: Logging Core
- Deliverables:
  - structured log event helper (intent/result)
  - consistent schema and serializers

## WS2: Job Log Sessions
- Deliverables:
  - per-job file lifecycle (`active` -> `success`/`failed`)
  - writer fan-out to existing runtime outputs

## WS3: Retention Enforcement
- Deliverables:
  - retention config defaults
  - periodic sweeper (default 60 minutes)
  - one-shot sweep hooks on command startup

## WS4: Runtime Instrumentation
- Deliverables:
  - pipeline/enricher/publisher key action instrumentation
  - clearer success/failure outcomes for instagram/facebook/threads

## WS5: Env + Validation + Memory
- Deliverables:
  - `.env.sample` + `.env` defaults
  - tests and full suite pass
  - append `TECHNICAL.md`

## Execution Order
1. WS1
2. WS2
3. WS3
4. WS4
5. WS5

## Resume Checklist
- [x] Directive created
- [x] Orchestration created
- [x] WS1 completed
- [x] WS2 completed
- [x] WS3 completed
- [x] WS4 completed
- [x] WS5 completed

## Learnings (append-only)
- 60-minute periodic sweep works well in long-running modes (`ppw` TUI, `ppw watch`), while one-shot sweep on command startup keeps short-lived commands clean without extra goroutine churn.
- Instrumenting only key domain actions (pipeline steps, enrichment components, publish/syndication outcomes) yields useful granularity without flooding every helper path.
