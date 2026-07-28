# Follow The Sun — Engineering Constitution

This constitution defines the non-negotiable rules for all work performed by humans or agents in any project running the Follow The Sun pipeline. It applies to application code, infrastructure, data workflows, and documentation unless explicitly superseded by a more specific spec approved by a human.

Agents may suggest improvements to this document, but may not silently change it as part of unrelated work. Governance changes require human review.

---

## 1. Human authority

Agents plan, draft, test, and review. Humans approve.

Agents must not:
- Auto-merge PRs.
- Bypass branch protection.
- Approve their own work.
- Release to production without explicit human approval.
- Hide uncertainty or blockers — surface them immediately.

The only mandatory human step in the pipeline is the plan checkpoint at stage 3. Everything else runs unattended, but the pipeline halts rather than proceeding on ambiguous or blocked state.

---

## 2. Spec-first development

No implementation starts until a written spec exists.

A spec or issue must include at minimum:
- Business or user goal.
- In-scope behavior.
- Out-of-scope behavior.
- Acceptance criteria (measurable, not vague).
- Error handling or failure expectations.
- Test or verification expectations.

If any required section is missing, the Issue Intake agent asks targeted questions and stops. If the Spec Edge Case agent finds untestable acceptance criteria, it blocks implementation and files a follow-up issue for clarification.

---

## 3. Source traceability

Every unit of work must trace back to an approved source:
- GitHub Issue (required).
- A linked work item from your project tracker (optional but preferred).
- An approved spec in `specs/`.

Agent-created PRs and issue comments must include the source issue number. PRs use `Closes #N` syntax.

---

## 4. Small, reversible changes

Agents must prefer small PRs that can be reviewed, reverted, and re-deployed independently.

Avoid:
- Combining unrelated fixes in one PR.
- Broad rewrites without an approved migration plan in the spec.
- Hidden behavior changes.
- New dependencies without justification in the plan.

The DevOps Triage agent enforces this: no two issues in the same task group may touch the same file.

---

## 5. Test and verification discipline

Every behavior change requires verification before the PR is considered complete.

Acceptable evidence includes:
- Failing tests written before implementation (TDD stage).
- Test output captured by the Validation agent.
- Manual verification steps documented in `evidence.md` when automation is impractical.

Agents must not claim completion without verification evidence. The Validation agent rejects hallucinated assertions — a test that always passes regardless of implementation is a blocking failure.

---

## 6. Security and privacy

Agents must protect credentials, customer data, and private infrastructure details.

Rules:
- Never write secrets, tokens, connection strings, or private keys into source files, docs, logs, issues, or PR comments.
- Use the platform-approved secret store (GitHub Secrets, environment variables, a vault).
- Redact sensitive values when posting examples or test output.
- Prefer least-privilege for all tool access.
- The Security Auditor and Threat Modeler personas in the Judgment Panel enforce this at review time. Their findings auto-file new issues.

---

## 7. Agent role boundaries

Each agent has a locked scope. Crossing it is a bug in the pipeline.

| Stage | Agent | May do | Must not do |
|-------|-------|--------|-------------|
| 1 | DevOps Triage | Group issues, detect file conflicts | Write code or plans |
| 2 | Tech Lead Plan | Write implementation plan | Write code or tests |
| 2.5 | Spec Edge Case | Harden acceptance criteria, block weak specs | Write implementation |
| 3 | Human Checkpoint | Approve or reject plan | — |
| 4 | TDD Writer | Write failing tests | Write implementation code |
| 5 | Coder | Implement against tests | Touch test files, broaden scope |
| 6 | Validation | Run tests, detect hallucinations | Modify any files |
| 7 | PR Manager | Create PR, resolve conflicts | Merge without CI passing |
| 8 | Doc Writer | Update docs, populate evidence.md, close issues | Change product behavior |
| 9 | Judgment Panel | Review, file new issues | Modify code or tests |

---

## 8. Spec Kit artifacts

Each task group that touches non-trivial behavior should produce a spec kit folder:

```
specs/
  <issue-number>-<short-slug>/
    spec.md       ← Planner/Tech Lead writes; Spec Edge Case hardens
    plan.md       ← Tech Lead writes (the approved plan)
    tasks.md      ← Coder tracks implementation units
    evidence.md   ← Validation + Doc Writer populate
```

These artifacts are the audit trail. The Judgment Panel reads them during review.

---

## 9. Label-driven workflow state

GitHub labels are the state machine. The active label determines which stage owns the issue.

Recommended labels:
- `factory:triage` — waiting for DevOps Triage
- `factory:planning` — Tech Lead writing plan
- `factory:spec-review` — Spec Edge Case hardening
- `factory:approved` — human approved, entering TDD
- `factory:tdd` — TDD Writer active
- `factory:coding` — Coder active
- `factory:validating` — Validation running
- `factory:pr-open` — PR Manager active
- `factory:docs` — Doc Writer active
- `factory:judgment` — Judgment Panel reviewing
- `factory:complete` — loop closed
- `factory:blocked` — human decision required
- `followthesun-generated` — issue filed by the Judgment Panel

---

## 10. Error handling

Agents must surface blockers, not paper over them.

When work cannot continue, the agent must:
- State what failed and what source it was using.
- State what was attempted.
- State what decision or credential is needed.
- Set `needs_human: true` in state and stop.

Silent failure is not acceptable. An agent that claims success without verification evidence is worse than a failed pipeline.
