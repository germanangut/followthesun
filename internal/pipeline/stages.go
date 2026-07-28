package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/brillio/nightshift/internal/agents"
	"github.com/brillio/nightshift/internal/checkpoint"
	gh "github.com/brillio/nightshift/internal/github"
	"github.com/brillio/nightshift/internal/state"
)

type Runner struct {
	ProjectRoot string
	StateFile   string
	State       *state.RunState
}

func (r *Runner) RunStage(stage state.Stage) error {
	fmt.Printf("\n[nightshift] Stage: %s\n", stage)
	switch stage {
	case state.StageDevOpsTriage:
		return r.stageDevOpsTriage()
	case state.StageTechLeadPlan:
		return r.stageTechLeadPlan()
	case state.StageHumanCheckpoint:
		return r.stageHumanCheckpoint()
	case state.StageTDDWriter:
		return r.stageTDDWriter()
	case state.StageCoder:
		return r.stageCoder()
	case state.StageValidation:
		return r.stageValidation()
	case state.StagePRManager:
		return r.stagePRManager()
	case state.StageDocWriter:
		return r.stageDocWriter()
	case state.StageJudgment:
		return r.stageJudgment()
	}
	return fmt.Errorf("unknown stage: %s", stage)
}

// --- Stage 1: DevOps Triage ---

func (r *Runner) stageDevOpsTriage() error {
	rawIssues, err := gh.ListOpenIssues(r.State.Repo)
	if err != nil {
		return fmt.Errorf("listing issues: %w", err)
	}

	ctx, _ := json.Marshal(map[string]any{
		"repo":   r.State.Repo,
		"issues": rawIssues,
	})

	var result struct {
		TaskGroups []state.TaskGroup `json:"task_groups"`
		Issues     []state.Issue     `json:"issues"`
	}
	if err := agents.RunAndParse(r.ProjectRoot, agents.RunOptions{
		SkillName:   "devops-triage",
		ContextJSON: ctx,
	}, &result); err != nil {
		return err
	}

	r.State.Issues = result.Issues
	r.State.TaskGroups = result.TaskGroups
	r.State.NextStage()
	return r.State.Save(r.StateFile)
}

// --- Stage 2: Tech Lead Plan ---

func (r *Runner) stageTechLeadPlan() error {
	for i := range r.State.TaskGroups {
		ctx, _ := json.Marshal(map[string]any{
			"repo":       r.State.Repo,
			"task_group": r.State.TaskGroups[i],
		})

		var result struct {
			Plan         string   `json:"plan"`
			FilesTouched []string `json:"files_touched"`
			SpecPath     string   `json:"spec_path"`
		}
		if err := agents.RunAndParse(r.ProjectRoot, agents.RunOptions{
			SkillName:   "tech-lead-plan",
			ContextJSON: ctx,
			Model:       "claude-opus-4-8",
		}, &result); err != nil {
			return err
		}

		tg := &r.State.TaskGroups[i]
		tg.Plan = result.Plan
		tg.FilesTouched = result.FilesTouched
		tg.SpecPath = result.SpecPath

		// The agent writes specs/<id>/plan.md straight to disk while we're
		// still on main. Commit it here, before any branch is cut, so it
		// doesn't sit untracked and leak into the first task group's branch
		// the way plan.md files were doing.
		if result.SpecPath != "" {
			if err := commitFiles([]string{result.SpecPath}, fmt.Sprintf("nightshift: plan for %s", issueNums(tg.Issues))); err != nil {
				return err
			}
		}
	}

	r.State.NextStage()
	return r.State.Save(r.StateFile)
}

// --- Stage 3: Human Checkpoint ---

func (r *Runner) stageHumanCheckpoint() error {
	allPlans := strings.Builder{}
	for i, tg := range r.State.TaskGroups {
		allPlans.WriteString(fmt.Sprintf("\n## Task Group %d (issues: %s)\n\n", i+1, issueNums(tg.Issues)))
		allPlans.WriteString(tg.Plan)
		allPlans.WriteString("\n")
	}

	approved := checkpoint.Gate(allPlans.String())
	if !approved {
		os.Exit(0)
	}

	r.State.CheckpointApproved = true
	r.State.NextStage()
	return r.State.Save(r.StateFile)
}

// --- Stage 4: TDD Writer ---

func (r *Runner) stageTDDWriter() error {
	for i := range r.State.TaskGroups {
		tg := &r.State.TaskGroups[i]

		// Refuse to switch branches on a dirty tree: an uncommitted or
		// untracked file from a previous task group would silently ride
		// along into this group's branch via git checkout.
		if err := ensureCleanTree(fmt.Sprintf("before starting task group %s", tg.ID)); err != nil {
			return err
		}

		// Cut the task group's branch here, before any agent writes a single
		// file, so every commit from this point on (tests, implementation,
		// docs) lands on an isolated branch instead of main.
		if tg.PRBranch == "" {
			branch := fmt.Sprintf("nightshift/%s-%d", r.State.RunID[:8], i)
			exists, err := branchExists(branch)
			if err != nil {
				return err
			}
			if exists {
				// A prior run got far enough to cut this branch but crashed
				// before its state was saved (state is only persisted once a
				// task group's work fully completes). Treat the branch as
				// already-cut instead of failing on "branch already exists".
				if err := runGit("checkout", branch); err != nil {
					return err
				}
			} else if err := runGit("checkout", "-b", branch, "main"); err != nil {
				return err
			}
			tg.PRBranch = branch
			if err := r.State.Save(r.StateFile); err != nil {
				return err
			}
		} else if err := runGit("checkout", tg.PRBranch); err != nil {
			return err
		}

		ctx, _ := json.Marshal(map[string]any{
			"repo":       r.State.Repo,
			"task_group": r.State.TaskGroups[i],
		})

		var result struct {
			TestFiles []string `json:"test_files"`
		}
		if err := agents.RunAndParse(r.ProjectRoot, agents.RunOptions{
			SkillName:   "tdd-writer",
			ContextJSON: ctx,
		}, &result); err != nil {
			return err
		}

		if len(result.TestFiles) == 0 {
			return fmt.Errorf("tdd writer reported no test files for task group %s", tg.ID)
		}
		tg.TestFiles = result.TestFiles

		// Commit deterministically in Go rather than trusting the agent's
		// own git commit: the Go binary owns ordering and state, and a
		// forgotten or partial agent commit is exactly what leaks a task
		// group's files into the next group's branch.
		if err := commitFiles(result.TestFiles, fmt.Sprintf("nightshift: tests for %s", issueNums(tg.Issues))); err != nil {
			return err
		}

		// Persist per task group, not just once at the end of the loop: a
		// later group's failure must not lose an earlier group's completed
		// work on resume.
		if err := r.State.Save(r.StateFile); err != nil {
			return err
		}
	}

	r.State.NextStage()
	return r.State.Save(r.StateFile)
}

// --- Stage 5: Coder ---

func (r *Runner) stageCoder() error {
	for i := range r.State.TaskGroups {
		tg := &r.State.TaskGroups[i]
		if err := ensureCleanTree(fmt.Sprintf("before starting task group %s", tg.ID)); err != nil {
			return err
		}
		if err := runGit("checkout", tg.PRBranch); err != nil {
			return err
		}

		ctx, _ := json.Marshal(map[string]any{
			"repo":       r.State.Repo,
			"task_group": r.State.TaskGroups[i],
		})

		var result struct {
			Status      string   `json:"status"`
			ImplFiles   []string `json:"impl_files"`
			TestSummary string   `json:"test_summary"`
			Concerns    string   `json:"concerns"`
		}
		if err := agents.RunAndParse(r.ProjectRoot, agents.RunOptions{
			SkillName:   "coder",
			ContextJSON: ctx,
		}, &result); err != nil {
			return err
		}

		tg.ImplFiles = result.ImplFiles
		tg.CoderStatus = result.Status
		tg.CoderConcerns = result.Concerns

		if result.Status == "BLOCKED" || result.Status == "NEEDS_CONTEXT" {
			_ = r.State.Save(r.StateFile)
			return fmt.Errorf("coder halted on task group %s: status=%s concerns=%s", tg.ID, result.Status, result.Concerns)
		}

		if err := commitFiles(result.ImplFiles, fmt.Sprintf("nightshift: implementation for %s", issueNums(tg.Issues))); err != nil {
			return err
		}

		if err := r.State.Save(r.StateFile); err != nil {
			return err
		}
	}

	r.State.NextStage()
	return r.State.Save(r.StateFile)
}

// --- Stage 6: Validation ---

func (r *Runner) stageValidation() error {
	for i := range r.State.TaskGroups {
		tg := &r.State.TaskGroups[i]
		if err := ensureCleanTree(fmt.Sprintf("before starting task group %s", tg.ID)); err != nil {
			return err
		}
		if err := runGit("checkout", tg.PRBranch); err != nil {
			return err
		}

		ctx, _ := json.Marshal(map[string]any{
			"repo":       r.State.Repo,
			"task_group": r.State.TaskGroups[i],
		})

		var result struct {
			Pass       bool   `json:"pass"`
			TestOutput string `json:"test_output"`
			Reason     string `json:"reason,omitempty"`
		}
		if err := agents.RunAndParse(r.ProjectRoot, agents.RunOptions{
			SkillName:   "validate",
			ContextJSON: ctx,
		}, &result); err != nil {
			return err
		}

		tg.TestOutput = result.TestOutput

		if !result.Pass {
			_ = r.State.Save(r.StateFile)
			return fmt.Errorf("validation failed for group %d: %s", i, result.Reason)
		}

		if err := r.State.Save(r.StateFile); err != nil {
			return err
		}
	}

	r.State.NextStage()
	return r.State.Save(r.StateFile)
}

// --- Stage 7: PR Manager ---

func (r *Runner) stagePRManager() error {
	for i := range r.State.TaskGroups {
		tg := &r.State.TaskGroups[i]
		if err := ensureCleanTree(fmt.Sprintf("before starting task group %s", tg.ID)); err != nil {
			return err
		}

		// Tests and implementation were already committed to tg.PRBranch by
		// the TDD Writer and Coder stages; just push what's there.
		if err := runGit("checkout", tg.PRBranch); err != nil {
			return err
		}
		if err := runGit("push", "origin", tg.PRBranch); err != nil {
			return err
		}

		// A resumed run may have already opened this PR before crashing pre-Save;
		// don't file a duplicate.
		if tg.PRNumber == 0 {
			title := fmt.Sprintf("[nightshift] %s", issueNums(tg.Issues))
			prNum, prURL, err := gh.CreatePR(r.State.Repo, tg.PRBranch, "main", title, tg.Plan)
			if err != nil {
				return err
			}

			tg.PRNumber = prNum
			tg.PRURL = prURL
			fmt.Printf("  PR created: %s\n", prURL)
		}

		if err := r.State.Save(r.StateFile); err != nil {
			return err
		}
	}

	r.State.NextStage()
	return r.State.Save(r.StateFile)
}

// --- Stage 8: Doc Writer ---

func (r *Runner) stageDocWriter() error {
	for i := range r.State.TaskGroups {
		tg := &r.State.TaskGroups[i]
		if err := ensureCleanTree(fmt.Sprintf("before starting task group %s", tg.ID)); err != nil {
			return err
		}
		if err := runGit("checkout", tg.PRBranch); err != nil {
			return err
		}

		ctx, _ := json.Marshal(map[string]any{
			"repo":       r.State.Repo,
			"task_group": r.State.TaskGroups[i],
		})

		var result struct {
			DocsUpdated  []string `json:"docs_updated"`
			EvidencePath string   `json:"evidence_path"`
		}
		if err := agents.RunAndParse(r.ProjectRoot, agents.RunOptions{
			SkillName:   "doc-writer",
			ContextJSON: ctx,
		}, &result); err != nil {
			return err
		}

		tg.DocsUpdated = result.DocsUpdated

		addFiles := append([]string{}, result.DocsUpdated...)
		if result.EvidencePath != "" {
			addFiles = append(addFiles, result.EvidencePath)
		}
		if len(addFiles) > 0 {
			if err := commitFiles(addFiles, fmt.Sprintf("nightshift: docs for %s", issueNums(tg.Issues))); err != nil {
				return err
			}
			if err := runGit("push", "origin", tg.PRBranch); err != nil {
				return err
			}
		}

		if err := r.State.Save(r.StateFile); err != nil {
			return err
		}
	}

	r.State.NextStage()
	return r.State.Save(r.StateFile)
}

// --- Stage 9: Judgment Panel ---

var judgmentPanels = []struct {
	Persona string
	Model   string
}{
	{"Security Auditor", "claude-opus-4-8"},
	{"Threat Modeler", "claude-opus-4-8"},
	{"Architecture Reviewer", "claude-sonnet-4-6"},
	{"Performance Reviewer", "claude-sonnet-4-6"},
	{"Correctness Reviewer", "claude-haiku-4-5-20251001"},
}

func (r *Runner) stageJudgment() error {
	allPass := true

	for _, tg := range r.State.TaskGroups {
		for _, panel := range judgmentPanels {
			ctx, _ := json.Marshal(map[string]any{
				"repo":       r.State.Repo,
				"task_group": tg,
				"persona":    panel.Persona,
			})

			var result struct {
				Pass      bool          `json:"pass"`
				Findings  string        `json:"findings"`
				NewIssues []state.Issue `json:"new_issues,omitempty"`
			}
			if err := agents.RunAndParse(r.ProjectRoot, agents.RunOptions{
				SkillName:   "judgment-panel",
				ContextJSON: ctx,
				Model:       panel.Model,
			}, &result); err != nil {
				return err
			}

			verdict := state.JudgmentVerdict{
				Panel:     fmt.Sprintf("%s / %s", panel.Persona, tg.ID),
				Model:     panel.Model,
				Persona:   panel.Persona,
				Pass:      result.Pass,
				Findings:  result.Findings,
				NewIssues: result.NewIssues,
			}
			r.State.Verdicts = append(r.State.Verdicts, verdict)

			if !result.Pass {
				allPass = false
				fmt.Printf("  [FAIL] %s (%s): %s\n", panel.Persona, panel.Model, result.Findings)

				// Auto-create new issues for failures
				for _, issue := range result.NewIssues {
					body := fmt.Sprintf("**Auto-generated by nightshift judgment panel**\n\nPanel: %s\nOriginal issue(s): %s\n\n%s",
						panel.Persona, issueNums(tg.Issues), issue.Body)
					num, err := gh.CreateIssue(r.State.Repo, issue.Title, body, issue.Type, issue.Priority)
					if err != nil {
						fmt.Printf("  Warning: failed to create issue: %v\n", err)
						continue
					}
					issue.Number = num
					r.State.GeneratedIssues = append(r.State.GeneratedIssues, issue)
					fmt.Printf("  Created issue #%d: %s\n", num, issue.Title)
				}
			} else {
				fmt.Printf("  [PASS] %s (%s)\n", panel.Persona, panel.Model)
			}

			// Avoid rate limiting between panel calls
			time.Sleep(2 * time.Second)
		}
	}

	if !allPass {
		fmt.Printf("\n[nightshift] %d new issues generated from judgment failures — they will enter the next pipeline run.\n",
			len(r.State.GeneratedIssues))
	}

	r.State.NextStage()
	return r.State.Save(r.StateFile)
}

func runGit(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %v: %w", args, err)
	}
	return nil
}

// branchExists reports whether a local branch with this name already exists.
func branchExists(branch string) (bool, error) {
	cmd := exec.Command("git", "show-ref", "--verify", "--quiet", "refs/heads/"+branch)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git show-ref %q: %w", branch, err)
}

// gitPorcelainStatus returns the working tree status in machine-readable form.
func gitPorcelainStatus() (string, error) {
	cmd := exec.Command("git", "status", "--porcelain")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git status --porcelain: %w", err)
	}
	return out.String(), nil
}

// ensureCleanTree fails loudly if the working tree has any uncommitted or
// untracked changes. Task groups run sequentially in one shared working
// directory, so a stray file left behind by one group (an agent that forgot
// to commit, or committed only part of what it wrote) would otherwise ride
// along silently on `git checkout` into the next group's branch.
func ensureCleanTree(context string) error {
	status, err := gitPorcelainStatus()
	if err != nil {
		return err
	}
	if strings.TrimSpace(status) != "" {
		return fmt.Errorf("working tree is not clean %s — refusing to continue since stray files would leak into the next task group's branch:\n%s", context, status)
	}
	return nil
}

// gitStagedFiles returns the names of files currently staged for commit.
func gitStagedFiles() (string, error) {
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git diff --cached --name-only: %w", err)
	}
	return out.String(), nil
}

// commitFiles stages exactly the given files and commits them, deterministically,
// rather than trusting the agent's own git commit. It then verifies the tree is
// clean afterward — if the agent wrote files outside this list, that surfaces
// here instead of leaking into the next task group.
//
// If the agent already committed these files itself (skill prompts historically
// told it to), `git add` stages no actual change — skip the commit rather than
// failing on git's "nothing to commit" exit code. An empty file list is also a
// no-op here: some stages (e.g. Coder, when the existing code already satisfies
// the tests) can legitimately have nothing to commit. Callers that require at
// least one file (e.g. TDD Writer must always produce a test) check that
// themselves before calling in.
func commitFiles(files []string, message string) error {
	if len(files) == 0 {
		return ensureCleanTree(fmt.Sprintf("after %q (no files reported)", message))
	}
	if err := runGit(append([]string{"add"}, files...)...); err != nil {
		return err
	}

	staged, err := gitStagedFiles()
	if err != nil {
		return err
	}
	if strings.TrimSpace(staged) != "" {
		if err := runGit("commit", "-m", message); err != nil {
			return err
		}
	}

	return ensureCleanTree(fmt.Sprintf("after committing %q", message))
}

func issueNums(issues []state.Issue) string {
	nums := make([]string, len(issues))
	for i, iss := range issues {
		nums[i] = fmt.Sprintf("#%d", iss.Number)
	}
	return strings.Join(nums, ", ")
}
