package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/germanangut/followthesun/internal/agents"
	gh "github.com/germanangut/followthesun/internal/github"
	"github.com/germanangut/followthesun/internal/pipeline"
	"github.com/germanangut/followthesun/internal/state"
	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

var (
	flagRepo        string
	flagStateFile   string
	flagProjectRoot string
	flagResume      bool
	flagDescribe    bool
)

func main() {
	root := &cobra.Command{
		Use:   "factory",
		Short: "Follow The Sun — AI agent assembly line orchestrator",
	}

	run := &cobra.Command{
		Use:   "run",
		Short: "Run the full pipeline",
		RunE:  runPipeline,
	}
	run.Flags().StringVar(&flagRepo, "repo", "", "GitHub repo (owner/name)")
	run.Flags().StringVar(&flagStateFile, "state", "factory-state.json", "Path to state file")
	run.Flags().StringVar(&flagProjectRoot, "project", ".", "Path to target project root")
	run.Flags().BoolVar(&flagResume, "resume", false, "Resume from existing state file")
	run.Flags().BoolVar(&flagDescribe, "describe", false, "Start from a manual description instead of open GitHub issues")
	run.MarkFlagRequired("repo")

	intake := &cobra.Command{
		Use:   "intake",
		Short: "Rewrite and file a raw issue description",
		RunE:  runIntake,
	}
	intake.Flags().StringVar(&flagRepo, "repo", "", "GitHub repo (owner/name)")
	intake.Flags().StringVar(&flagProjectRoot, "project", ".", "Path to target project root")
	intake.MarkFlagRequired("repo")

	status := &cobra.Command{
		Use:   "status",
		Short: "Print current pipeline status",
		RunE:  runStatus,
	}
	status.Flags().StringVar(&flagStateFile, "state", "factory-state.json", "Path to state file")

	root.AddCommand(run, intake, status)
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// readDescription reads multi-line input from r until a blank line is entered.
// The caller must pass a shared bufio.Reader so subsequent reads from the same
// stdin are not lost to the scanner's internal buffer.
func readDescription(r *bufio.Reader) (string, error) {
	fmt.Print("Describe what you want built (press Enter twice when done):\n\n")
	var lines []string
	for {
		line, err := r.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")
		if line == "" && len(lines) > 0 {
			return strings.Join(lines, "\n"), nil
		}
		if line != "" {
			lines = append(lines, line)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	return strings.Join(lines, "\n"), nil
}

// intakeResult is the structured output from the issue-intake agent.
type intakeResult struct {
	Title         string   `json:"title"`
	Body          string   `json:"body"`
	Type          string   `json:"type"`
	Priority      string   `json:"priority"`
	Labels        []string `json:"labels"`
	Blocked       bool     `json:"blocked"`
	BlockedReason string   `json:"blocked_reason"`
}

// runDescribeAndFile prompts for a description, runs the intake agent, confirms with the user,
// and optionally files the issue to GitHub. Returns the intake result and the filed issue number
// (0 if the user chose to continue without filing).
func runDescribeAndFile(absRoot string) (*intakeResult, int, error) {
	// One shared reader for all stdin I/O in this function; creating separate
	// bufio.Scanners would each buffer ahead on the pipe and lose bytes.
	r := bufio.NewReader(os.Stdin)

	description, err := readDescription(r)
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(description) == "" {
		return nil, 0, fmt.Errorf("no description provided")
	}

	fmt.Println("\nRunning intake agent...")

	ctx, _ := json.Marshal(map[string]any{"description": description})
	var result intakeResult
	if err := agents.RunAndParse(absRoot, agents.RunOptions{
		SkillName:   "issue-intake",
		ContextJSON: ctx,
	}, &result); err != nil {
		return nil, 0, err
	}

	if result.Blocked {
		fmt.Printf("\n⚠  Intake blocked — acceptance criteria need to be sharpened:\n\n%s\n", result.BlockedReason)
		fmt.Println("\nRevise your description and try again.")
		return nil, 0, fmt.Errorf("intake blocked: %s", result.BlockedReason)
	}

	fmt.Printf("\n── Structured issue ──────────────────────────────────────────\n")
	fmt.Printf("Title:    %s\n", result.Title)
	fmt.Printf("Type:     %s   Priority: %s\n", result.Type, result.Priority)
	fmt.Printf("Labels:   %s\n", strings.Join(result.Labels, ", "))
	fmt.Printf("\n%s\n", result.Body)
	fmt.Printf("──────────────────────────────────────────────────────────────\n")

	fmt.Print("\nFile this issue to GitHub? [y/c/N] (c = continue pipeline without filing): ")
	choiceLine, _ := r.ReadString('\n')
	choice := strings.ToLower(strings.TrimSpace(choiceLine))

	switch choice {
	case "y":
		num, err := gh.CreateIssue(flagRepo, result.Title, result.Body, result.Type, result.Priority)
		if err != nil {
			return nil, 0, fmt.Errorf("filing issue: %w", err)
		}
		fmt.Printf("\nFiled as #%d\n", num)
		return &result, num, nil
	case "c":
		fmt.Println("\nContinuing without filing to GitHub.")
		return &result, 0, nil
	default:
		return nil, 0, fmt.Errorf("aborted")
	}
}

func runPipeline(cmd *cobra.Command, args []string) error {
	absRoot, err := filepath.Abs(flagProjectRoot)
	if err != nil {
		return err
	}

	var s *state.RunState

	if flagResume {
		s, err = state.Load(flagStateFile)
		if err != nil {
			return fmt.Errorf("cannot resume: %w", err)
		}
		fmt.Printf("[followthesun] Resuming from stage: %s\n", s.Stage)
	} else {
		startStage := state.StageDevOpsTriage

		s = &state.RunState{
			RunID:     uuid.New().String(),
			Repo:      flagRepo,
			Stage:     startStage,
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}

		if flagDescribe {
			intake, issueNum, err := runDescribeAndFile(absRoot)
			if err != nil {
				return err
			}
			if issueNum == 0 {
				// User chose to skip filing — seed the state directly and bypass DevOpsTriage.
				synthetic := state.Issue{
					Number:   0,
					Title:    intake.Title,
					Body:     intake.Body,
					Type:     intake.Type,
					Priority: intake.Priority,
					Labels:   intake.Labels,
				}
				s.Issues = []state.Issue{synthetic}
				s.TaskGroups = []state.TaskGroup{{
					ID:     "local-" + s.RunID[:8],
					Issues: []state.Issue{synthetic},
				}}
				s.Stage = state.StageTechLeadPlan
				fmt.Println("\n[followthesun] Skipping GitHub — starting pipeline at tech-lead-plan...")
			} else {
				fmt.Println("\n[followthesun] Issue filed — starting pipeline...")
			}
		}

		if err := s.Save(flagStateFile); err != nil {
			return err
		}
		fmt.Printf("[followthesun] Starting new run: %s\n", s.RunID)
	}

	runner := &pipeline.Runner{
		ProjectRoot: absRoot,
		StateFile:   flagStateFile,
		State:       s,
	}

	for s.Stage != state.StageDone {
		if err := runner.RunStage(s.Stage); err != nil {
			fmt.Fprintf(os.Stderr, "[followthesun] Stage %s failed: %v\n", s.Stage, err)
			fmt.Fprintf(os.Stderr, "State saved to %s — fix and re-run with --resume\n", flagStateFile)
			os.Exit(1)
		}
	}

	fmt.Printf("\n[followthesun] Pipeline complete. Run ID: %s\n", s.RunID)
	return nil
}

func runIntake(cmd *cobra.Command, args []string) error {
	absRoot, err := filepath.Abs(flagProjectRoot)
	if err != nil {
		return err
	}

	if _, _, err := runDescribeAndFile(absRoot); err != nil {
		return err
	}
	return nil
}

func runStatus(cmd *cobra.Command, args []string) error {
	s, err := state.Load(flagStateFile)
	if err != nil {
		return err
	}
	fmt.Printf("Run:     %s\n", s.RunID)
	fmt.Printf("Repo:    %s\n", s.Repo)
	fmt.Printf("Stage:   %s\n", s.Stage)
	fmt.Printf("Groups:  %d\n", len(s.TaskGroups))
	fmt.Printf("Issues:  %d\n", len(s.Issues))
	if len(s.Verdicts) > 0 {
		pass, fail := 0, 0
		for _, v := range s.Verdicts {
			if v.Pass {
				pass++
			} else {
				fail++
			}
		}
		fmt.Printf("Verdicts: %d pass / %d fail\n", pass, fail)
	}
	return nil
}
