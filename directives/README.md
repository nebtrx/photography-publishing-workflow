# Directives

Directives are SOPs written in Markdown. They define *what to do* (goals, constraints, inputs/outputs, edge cases, acceptance criteria).
Claude (or another agent) acts as orchestrator: it reads a directive and uses deterministic tools in `execution/` to do the work.

## Format
Each directive should include:
- Goal
- Context / Constraints
- Inputs (files, env vars, params)
- Outputs (artifacts, files, reports)
- Steps (high-level)
- Edge cases / Failure modes
- Acceptance criteria
- Safety notes (if relevant)

## Policy
- Directives are living documents, but don’t rewrite/replace them unless instructed.
- Prefer adding “Learnings” sections rather than rewriting history.
