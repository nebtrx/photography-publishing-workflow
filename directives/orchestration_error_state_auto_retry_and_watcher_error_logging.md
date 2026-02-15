# Orchestration: Error-State Auto-Retry and Watcher Error Logging

## Phase
Orchestration + execution in current session.

## Objective
Execute `directives/error_state_auto_retry_and_watcher_error_logging.md` with no regression to existing watcher behavior.

## Workstreams

## WS1: Watcher Runtime Behavior
- Deliverables:
  - monitor post directories for file-level image changes
  - debounced retry for `state:error` directories
- Risk:
  - duplicate triggers
- Mitigation:
  - per-directory debounce timer + forced retry gating only on image change

## WS2: Error Observability
- Deliverables:
  - improved startup logs for existing errored manifests
  - manifest-derived reason text in watcher logs

## WS3: Validation
- Deliverables:
  - tests for auto-retry on image change
  - full suite pass
  - memory update in `TECHNICAL.md`

## Execution Order
1. WS1
2. WS2
3. WS3

## Resume Checklist
- [x] Directive created
- [x] Orchestration created
- [x] WS1 completed
- [x] WS2 completed
- [x] WS3 completed

## Learnings (append-only)
- Recovery behavior should only force retries for image-file changes, not generic manifest churn.
