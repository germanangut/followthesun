# Follow The Sun — Engineering Discipline

Read `CONSTITUTION.md` before anything else. It defines the non-negotiable rules that override all other instructions.

## Operating model

50-100 agents run simultaneously. Every step is deterministic — the Go binary owns ordering and state. Prompts shape persona behavior only; they do not replace tooling.

## Pipeline stages

1. `/devops-triage` — groups non-conflicting backlog issues by file ownership
2. `/tech-lead-plan` — writes implementation plan (Opus 4.8); writes `specs/<id>/plan.md`
3. `/spec-edge-case` — hardens acceptance criteria; blocks on ambiguity before human sees the plan
4. Human checkpoint — only manual step; binary blocks on stdin
5. `/tdd-writer` — writes failing tests; no implementation
6. `/coder` — implements against tests; cannot touch test files
7. `/validate` — rejects hallucinated asserts; must see real failures before implementation
8. `/pr-manager` — opens PR, resolves conflicts
9. `/doc-writer` — updates docs, populates `specs/<id>/evidence.md`, closes issues
10. `/judgment-panel` — 5 personas × 2-3 models; failures generate new issues

## Invariants

- No agent writes to a file owned by another task group
- No agent modifies test files after the TDD stage
- No implementation starts without measurable acceptance criteria (enforced at intake)
- The plan is hardened by Spec Edge Case and approved by a human before any code is written
- Judgment panel failures auto-generate GitHub issues and loop back to stage 1

## Spec Kit artifacts

Each task group produces a spec folder:

```
specs/
  <issue-number>-<short-slug>/
    spec.md       ← written if issue came through intake with a full spec; otherwise derived from issue
    plan.md       ← written by Tech Lead Plan at stage 2
    evidence.md   ← written by Doc Writer at stage 9
```

## Running the factory

```bash
# Run full pipeline
factory run --repo owner/repo --project /path/to/project

# Resume after a failure
factory run --repo owner/repo --project /path/to/project --resume

# File a structured issue from raw text
factory intake --repo owner/repo

# Check pipeline status
factory status
```

## Just-in-time token loading

Each agent receives only the context it needs. Never pass the full issue list to the coder. Never pass impl files to the TDD writer. The Spec Edge Case agent sees only the plan and the source issues — not the test files or implementation.

## Issue intake

Users describe issues informally. The `/issue-intake` skill rewrites, classifies, and prioritizes before filing. It enforces the spec-first gate: untestable acceptance criteria block filing until the requester makes them measurable.

## Label-driven workflow state

Use GitHub labels to track pipeline position. See `CONSTITUTION.md` section 9 for the full label list. Agents apply the next label on handoff and remove the current one.
