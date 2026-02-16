# Orchestration: Publish `code=9004` Child Container Failure

## Phase
Orchestration only. No implementation changes in this artifact.

## Objective
Execute `directives/publish_code9004_child_container_failure.md` with a reproducible diagnosis path, targeted safeguards, and test-backed remediation.

## Scope
- In scope:
  - root-cause diagnosis for `code=9004` failures
  - media preflight and diagnostic hardening
  - publisher/validator safeguards
  - test coverage and operational logging improvements
- Out of scope:
  - broad social-publishing redesign
  - unrelated TUI layout/UX changes

## Current Baseline (confirmed)
- Failure log indicates:
  - module `publisher`, action `publish_instagram`
  - failure at `create child container 1`
  - Meta `code=9004` media-type rejection.
- Existing retries and error-state handling are present but root-cause signaling is still insufficient.

## Workstreams

## WS1: Reproduction and Failure Taxonomy
- Owner: publish diagnostics
- Deliverables:
  - reproduce with failing manifest/job log
  - classify possible root causes (bad media bytes, bad content-type, bad URL accessibility, mixed media params)
- Dependencies: none
- Output contract:
  - failure matrix with evidence signatures

## WS2: Preflight Media Verification
- Owner: publisher/hosting
- Deliverables:
  - preflight checks for each media candidate prior to container creation
  - early fail with actionable details
- Dependencies: WS1
- Output contract:
  - prevents opaque API failures when media payload is invalid

## WS3: Publish-Step Diagnostics
- Owner: publisher/logging
- Deliverables:
  - structured logs include child index, filename, URL, detected media type
  - failure persistence includes same context
- Dependencies: WS2
- Output contract:
  - operators can identify the exact broken item quickly

## WS4: Guardrails and Retry Compatibility
- Owner: validator/state
- Deliverables:
  - validator/publisher guardrails for unsupported media edge cases
  - retry path remains deterministic with improved failure metadata
- Dependencies: WS2, WS3
- Output contract:
  - safe retries after external file fixes

## WS5: Tests and Verification
- Owner: quality
- Deliverables:
  - regression tests for `code=9004` class failures
  - end-to-end checks for diagnostics and publish success on valid media
  - technical log update in `TECHNICAL.md`
- Dependencies: WS1-WS4
- Output contract:
  - passing test suite + reproducible validation script

## Execution Order
1. WS1
2. WS2
3. WS3
4. WS4
5. WS5

## Handoff Gates
- Gate A:
  - reproduction and failure taxonomy complete.
- Gate B:
  - preflight checks added and verified.
- Gate C:
  - publish diagnostics include per-child context.
- Gate D:
  - guardrails/retry compatibility validated.
- Gate E:
  - tests pass and memory log updated.

## Risks and Mitigations
- Risk: intermittent external API behavior obscures root cause.
  - Mitigation: enforce local preflight signals and deterministic logs.
- Risk: over-logging increases noise.
  - Mitigation: structured fields + concise human summary.
- Risk: false rejects in preflight.
  - Mitigation: unit tests with valid/invalid fixtures and clear allow-list rules.

## Resume Checklist (for execution session)
- [x] Directive created
- [x] Orchestration created
- [ ] Implement WS1 reproduction matrix
- [ ] Implement WS2 preflight verification
- [ ] Implement WS3 diagnostics
- [ ] Implement WS4 guardrails/retry compatibility
- [ ] Run WS5 verification + memory update

## Learnings (append-only)
- Add confirmed platform quirks and the final root cause signature during execution.
