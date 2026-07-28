# Follow The Sun

> A deterministic Go binary that orchestrates an AI agent assembly line — 50–100 agents running simultaneously, ~20 projects in parallel, 0 lines hand-coded, 1 human in the loop.

The name comes from how it runs: you approve a plan, then the pipeline works through the night — writing tests, implementing, reviewing, opening PRs — and hands you finished work in the morning.

---

## How it works

The factory runs a 10-stage pipeline for every batch of GitHub issues. Each stage is a Claude agent with a locked role. The Go binary enforces ordering — no agent can skip ahead or run out of sequence.

```
 1. DevOps Triage    → groups non-conflicting issues by file ownership
 2. Tech Lead Plan   → writes implementation plan per group (Opus); writes specs/<id>/plan.md
 2.5 Spec Edge Case  → hardens acceptance criteria; blocks ambiguity before the human sees the plan
 3. Human Checkpoint → YOU review and approve the hardened plan  ← only manual step
 4. TDD Writer       → writes failing tests, no implementation
 5. Coder            → implements against tests, cannot touch test files
 6. Validation       → rejects hallucinated asserts, verifies real failures
 7. PR Manager       → opens PR, resolves merge conflicts
 8. Doc Writer       → updates docs, writes specs/<id>/evidence.md, closes issues
 9. Judgment Panel   → 5 personas × 3 models review for security/threat/arch/perf/correctness
                       failures auto-generate new issues → loop back to stage 1
```

Read `CONSTITUTION.md` for the non-negotiable rules governing all agents and humans in the pipeline.

---

## Setup

### Prerequisites

**macOS**

- [Go 1.22+](https://go.dev/dl/) — `brew install go`
- Git — ships with Xcode Command Line Tools (`xcode-select --install`), or `brew install git`
- [Claude Code CLI](https://claude.ai/code) — `npm install -g @anthropic-ai/claude-code`
- [GitHub CLI](https://cli.github.com/) — `brew install gh && gh auth login`
- [Headroom](https://github.com/headroomlabs-ai/headroom) — `pip install "headroom-ai[mcp,memory,ml,code,proxy]"`

**Windows (PowerShell)**

- [Go 1.22+](https://go.dev/dl/) — `winget install GoLang.Go`
- Git — `winget install Git.Git`
- Node.js (required for the Claude Code CLI) — `winget install OpenJS.NodeJS.LTS`
- [Claude Code CLI](https://claude.ai/code) — `npm install -g @anthropic-ai/claude-code`
- [GitHub CLI](https://cli.github.com/) — `winget install GitHub.cli` then `gh auth login`
- [Headroom](https://github.com/headroomlabs-ai/headroom) — `pip install "headroom-ai[mcp,memory,ml,code,proxy]"`

> After each `winget install`, open a new PowerShell window so the updated `PATH` takes effect before continuing.

### Install the binary

**macOS**

```bash
cd followthesun
make install        # builds factory → /usr/local/bin/factory

factory --help      # verify
```

**Windows (PowerShell)**

There's no `make` by default on Windows, so build with `go build` directly and put the binary on your `PATH` yourself:

```powershell
cd followthesun
go build -o bin\factory.exe .\cmd\factory

New-Item -ItemType Directory -Force "$env:LOCALAPPDATA\followthesun" | Out-Null
Copy-Item bin\factory.exe "$env:LOCALAPPDATA\followthesun\factory.exe" -Force
[Environment]::SetEnvironmentVariable("Path", "$env:Path;$env:LOCALAPPDATA\followthesun", "User")

# open a new PowerShell window, then:
factory --help      # verify
```

### Enable Headroom (token compression)

The factory runs 50–100 agents simultaneously. Headroom compresses all LLM traffic — tool outputs, file reads, search results — before it reaches the model. In practice this cuts 60–90% of the token volume on large runs.

**1. Register the MCP server** (one-time, user-scoped):

macOS:

```bash
headroom_bin=$(python3 -m site --user-base)/bin/headroom
claude mcp add headroom "$headroom_bin" mcp serve -s user
claude mcp get headroom   # should show Status: Connected
```

Windows (PowerShell):

```powershell
$headroomBin = "$(python -m site --user-base)\Scripts\headroom.exe"
claude mcp add headroom "$headroomBin" mcp serve -s user
claude mcp get headroom   # should show Status: Connected
```

**2. Add Headroom to your PATH** (one-time):

macOS:

```bash
echo 'export PATH="/Users/kenrickvaz/Library/Python/3.13/bin:$PATH"' >> ~/.zshrc
source ~/.zshrc
```

Windows (PowerShell):

```powershell
$pyScripts = "$(python -m site --user-base)\Scripts"
[Environment]::SetEnvironmentVariable("Path", "$env:Path;$pyScripts", "User")
# open a new PowerShell window to pick up the change
```

**3. Start the proxy** in a dedicated terminal before running the factory (same command on both platforms):

```bash
headroom proxy --port 8787
```

The proxy intercepts every Anthropic API call and compresses large payloads automatically — no code changes required.

**4. Run the factory through the proxy:**

macOS:

```bash
ANTHROPIC_BASE_URL=http://127.0.0.1:8787 factory run --repo your-org/your-repo --project .
```

Set `ANTHROPIC_BASE_URL` in your shell profile to make it permanent:

```bash
echo 'export ANTHROPIC_BASE_URL=http://127.0.0.1:8787' >> ~/.zshrc
```

Windows (PowerShell):

```powershell
$env:ANTHROPIC_BASE_URL = "http://127.0.0.1:8787"
factory run --repo your-org/your-repo --project .
```

Set it permanently for your user account:

```powershell
[Environment]::SetEnvironmentVariable("ANTHROPIC_BASE_URL", "http://127.0.0.1:8787", "User")
```

**5. Check savings:**

```bash
headroom dashboard   # live per-stage compression stats (proxy must be running)
headroom perf        # summary after a run
```

> **Without the proxy:** the Headroom MCP server (`headroom_compress`, `headroom_retrieve`, `headroom_stats`) is still available to agents on demand — you get compression when agents explicitly request it, even if the proxy is not running.

### Add skills to your project

Each project that uses the factory needs the skill files in its `.claude/commands/` directory. These are the prompt definitions for each agent role.

macOS:

```bash
cd /your/project
cp -r ~/Projects/followthesun/.claude/commands/ .claude/commands/

# Add to .gitignore
echo "factory-state.json" >> .gitignore
```

Windows (PowerShell):

```powershell
cd C:\your\project
Copy-Item -Recurse "$HOME\Projects\followthesun\.claude\commands" .claude\commands

# Add to .gitignore
Add-Content .gitignore "factory-state.json"
```

---

## Running the pipeline

There are two ways to start a run: pull from open GitHub issues, or describe what you want built.

### From GitHub issues

```bash
factory run --repo your-org/your-repo --project .
```

Pulls all open issues, groups them into non-conflicting batches, writes a plan, pauses for approval, then runs stages 4–9 automatically.

### From a description

```bash
factory run --repo your-org/your-repo --project . --describe
```

Prompts you to describe what you want built in plain language. The intake agent rewrites it into a structured GitHub issue with measurable acceptance criteria, shows you a preview, asks for confirmation, then starts the pipeline.

If the acceptance criteria aren't specific enough (e.g. "make it faster"), the intake agent blocks filing and tells you exactly what to sharpen. Fix the description and re-run.

You can also file an issue without starting the pipeline:

```bash
factory intake --repo your-org/your-repo
```

Also copy the constitution to your project if you haven't already:

macOS:

```bash
cp ~/Projects/followthesun/CONSTITUTION.md .
```

Windows (PowerShell):

```powershell
Copy-Item "$HOME\Projects\followthesun\CONSTITUTION.md" .
```

### Resume after a failure

Every stage writes state to `factory-state.json`. If a stage fails, fix the problem and resume:

```bash
factory run --repo your-org/your-repo --project . --resume
```

### Check status

```bash
factory status
# Run:     a3f2bc91-...
# Stage:   validation
# Groups:  4
# Issues:  12
```

---

## The human checkpoint

At stage 3, the pipeline pauses and prints the Tech Lead's plan:

```
================================================================================
  HUMAN CHECKPOINT — Step 3 of 9
================================================================================

## Task Group 1 (issues: #42, #43)
## Summary
Add rate limiting to the /login endpoint...

Approve this plan? [y/N/edit]:
```

| Response | Effect |
|---|---|
| `y` | Approve — pipeline continues |
| `n` | Reject — pipeline stops, update issues and re-run |
| `edit` | Opens `factory-state.json` for manual plan edits, then re-run with `--resume` |

**This is the only step that requires a human.** Everything else runs unattended.

---

## Spec Kit artifacts

Each task group generates a spec folder that becomes the audit trail:

```
specs/
  <issue-number>-<short-slug>/
    plan.md       ← Tech Lead writes at stage 2; Spec Edge Case hardens at stage 2.5
    evidence.md   ← Doc Writer writes at stage 8 after the PR merges
```

The Judgment Panel reads these during review. For complex features, teams can also add `spec.md` and `tasks.md` manually before running the pipeline.

---

## Judgment panel

After every merge, 5 panels review the changes:

| Persona | Model | Checks |
|---|---|---|
| Security Auditor | Opus | OWASP Top 10, injection, secret exposure |
| Threat Modeler | Opus | Attack surface, privilege escalation, trust boundaries |
| Architecture Reviewer | Sonnet | Coupling, pattern violations, blast radius |
| Performance Reviewer | Sonnet | N+1 queries, unbounded loops, blocking I/O |
| Correctness Reviewer | Haiku | Logic errors, nil dereferences, unhandled errors |

**Failures auto-file new GitHub issues** (labeled `nightshift-generated`) and loop back to stage 1 on the next run. No human triage needed.

---

## Engineering discipline

### Spec-first, constitution-governed
All work traces to a GitHub Issue with measurable acceptance criteria. The `CONSTITUTION.md` defines the rules no agent or human can override. The Spec Edge Case agent hardens every plan before a human checkpoint.

### Plan-driven + agentic
The Tech Lead writes a plan. The Spec Edge Case agent challenges it. A human approves it. Only then do agents build — at scale, in parallel. Agents expand on the plan but cannot contradict it.

### Deterministic tools over prompts
The Go binary is the source of truth. It owns:
- Stage ordering (cannot be skipped)
- File conflict detection (no two groups touch the same file)
- Human gate (blocking stdin)
- State persistence between stages

Prompts shape *how* agents behave, not *whether* they run.

### Just-in-time context loading
Each agent receives only the context it needs. The coder never sees the full issue list. The TDD writer never sees implementation files. Keeps token usage low and agents focused.

---

## Project layout

```
your-project/
├── CONSTITUTION.md             # non-negotiable rules; read before everything else
├── .claude/
│   └── commands/               # agent skill definitions
│       ├── issue-intake.md
│       ├── devops-triage.md
│       ├── tech-lead-plan.md
│       ├── spec-edge-case.md   # new: hardens plan before human checkpoint
│       ├── tdd-writer.md
│       ├── coder.md
│       ├── validate.md
│       ├── pr-manager.md
│       ├── doc-writer.md
│       └── judgment-panel.md
├── specs/                      # spec kit artifacts (committed)
│   └── <issue>-<slug>/
│       ├── plan.md
│       └── evidence.md
└── factory-state.json          # pipeline state (gitignored)
```

---

## Troubleshooting

**`connection refused` on port 8787** — the Headroom proxy isn't running. Start it with `headroom proxy --port 8787` in a separate terminal, or bypass it: macOS `unset ANTHROPIC_BASE_URL`, Windows `$env:ANTHROPIC_BASE_URL = $null`.

**`factory: command not found`** (macOS) — run `make install` from the followthesun repo, or ensure `/usr/local/bin` is in your `$PATH`.

**`'factory' is not recognized...`** (Windows) — the folder containing `factory.exe` isn't on your `PATH`. Re-run the "Install the binary" step above, then open a new PowerShell window.

**Stage fails with "skill not found"** — the `.claude/commands/` directory is missing from your project. Re-run the copy step above.

**Spec Edge Case blocks the plan** — expected behavior when acceptance criteria are ambiguous. Update the source issue to resolve the gap, then `--resume`.

**Issue intake returns `blocked: true`** — the acceptance criteria aren't measurable. Sharpen them (add baseline, metric, threshold) and re-run intake.

**Validation rejects tests** — the coder wrote trivially-passing assertions (`assert.True(t, true)` etc.). The state is saved; edit the test files, then `--resume`.

**Judgment panel generates issues every run** — expected behavior for recurring problems. Treat generated issues as P0/P1 work — they represent real gaps the panel found.

**PR has merge conflicts the agent can't resolve** — the `pr-manager` agent will set `needs_human: true` in state and stop. Resolve manually, push, then `--resume`.
