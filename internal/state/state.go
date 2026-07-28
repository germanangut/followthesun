package state

import (
	"encoding/json"
	"os"
	"time"
)

type Stage string

const (
	StageDevOpsTriage    Stage = "devops-triage"
	StageTechLeadPlan    Stage = "tech-lead-plan"
	StageHumanCheckpoint Stage = "human-checkpoint"
	StageTDDWriter       Stage = "tdd-writer"
	StageCoder           Stage = "coder"
	StageValidation      Stage = "validation"
	StagePRManager       Stage = "pr-manager"
	StageDocWriter       Stage = "doc-writer"
	StageJudgment        Stage = "judgment"
	StageDone            Stage = "done"
)

var StageOrder = []Stage{
	StageDevOpsTriage,
	StageTechLeadPlan,
	StageHumanCheckpoint,
	StageTDDWriter,
	StageCoder,
	StageValidation,
	StagePRManager,
	StageDocWriter,
	StageJudgment,
	StageDone,
}

type Issue struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Type     string `json:"type"`   // bug | chore | feature
	Priority string `json:"priority"` // P0 | P1 | P2 | P3
	Labels   []string `json:"labels"`
}

type TaskGroup struct {
	ID           string   `json:"id"`
	Issues       []Issue  `json:"issues"`
	FilesTouched []string `json:"files_touched"`
	Plan         string   `json:"plan"`
	SpecPath     string   `json:"spec_path,omitempty"`
	TestFiles    []string `json:"test_files"`
	ImplFiles    []string `json:"impl_files"`
	TestOutput   string   `json:"test_output"`
	PRNumber     int      `json:"pr_number,omitempty"`
	PRURL        string   `json:"pr_url,omitempty"`
	PRBranch     string   `json:"pr_branch,omitempty"`
	DocsUpdated    []string `json:"docs_updated"`
	CoderStatus    string   `json:"coder_status,omitempty"`    // DONE | DONE_WITH_CONCERNS | NEEDS_CONTEXT | BLOCKED
	CoderConcerns  string   `json:"coder_concerns,omitempty"`
}

type JudgmentVerdict struct {
	Panel       string `json:"panel"`
	Model       string `json:"model"`
	Persona     string `json:"persona"`
	Pass        bool   `json:"pass"`
	Findings    string `json:"findings"`
	NewIssues   []Issue `json:"new_issues,omitempty"`
}

type RunState struct {
	RunID              string          `json:"run_id"`
	Repo               string          `json:"repo"`
	Stage              Stage           `json:"stage"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	Issues             []Issue         `json:"issues"`
	TaskGroups         []TaskGroup     `json:"task_groups"`
	CurrentGroupIndex  int             `json:"current_group_index"`
	CheckpointApproved bool            `json:"checkpoint_approved"`
	Verdicts           []JudgmentVerdict `json:"verdicts"`
	GeneratedIssues    []Issue         `json:"generated_issues"` // from judgment failures
}

func Load(path string) (*RunState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s RunState
	return &s, json.Unmarshal(data, &s)
}

func (s *RunState) Save(path string) error {
	s.UpdatedAt = time.Now()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func (s *RunState) CurrentGroup() *TaskGroup {
	if s.CurrentGroupIndex >= len(s.TaskGroups) {
		return nil
	}
	return &s.TaskGroups[s.CurrentGroupIndex]
}

func (s *RunState) NextStage() {
	for i, stage := range StageOrder {
		if stage == s.Stage && i+1 < len(StageOrder) {
			s.Stage = StageOrder[i+1]
			return
		}
	}
}
