package agents

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type RunOptions struct {
	SkillName   string
	ContextJSON []byte
	Model       string // defaults to claude-sonnet-4-6
	MaxTokens   int
}

type RunResult struct {
	Output string
	Model  string
}

// Run invokes `claude --print` with a skill prompt and JSON context piped via stdin.
// The skill file is resolved from .claude/commands/<name>.md relative to the project root.
func Run(projectRoot string, opts RunOptions) (*RunResult, error) {
	skillPath := filepath.Join(projectRoot, ".claude", "commands", opts.SkillName+".md")
	skillContent, err := os.ReadFile(skillPath)
	if err != nil {
		return nil, fmt.Errorf("skill %q not found: %w", opts.SkillName, err)
	}

	model := opts.Model
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	// Build the prompt: skill instructions + context block
	prompt := string(skillContent)
	if opts.ContextJSON != nil {
		prompt += "\n\n<context>\n" + string(opts.ContextJSON) + "\n</context>"
	}

	out, err := runPrompt(prompt, model, opts.MaxTokens)
	if err != nil {
		return nil, fmt.Errorf("agent %q failed: %w", opts.SkillName, err)
	}

	return &RunResult{Output: out, Model: model}, nil
}

// runPrompt executes `claude --print` with the given prompt and returns its
// trimmed stdout.
func runPrompt(prompt, model string, maxTokens int) (string, error) {
	args := []string{"--print", "--model", model, "--dangerously-skip-permissions"}
	if maxTokens > 0 {
		args = append(args, "--max-tokens", fmt.Sprintf("%d", maxTokens))
	}

	cmd := exec.Command("claude", args...)
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Stderr = os.Stderr

	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%w\n%s", err, out.String())
	}

	return strings.TrimSpace(out.String()), nil
}

// RunAndParse runs an agent and unmarshals JSON from the output into dest.
// The agent output must be a JSON object (the skill prompt enforces this).
// If the model ignores the format instruction (prose instead of JSON, or
// JSON with a stray unescaped quote), one repair pass asks it to reformat
// its own prior output as strict JSON before giving up.
func RunAndParse(projectRoot string, opts RunOptions, dest any) error {
	result, err := Run(projectRoot, opts)
	if err != nil {
		return err
	}

	if err := parseAgentJSON(result.Output, dest); err == nil {
		return nil
	}

	model := opts.Model
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	fixPrompt := "Your previous response did not follow the required output format. " +
		"Reformat it as ONLY the JSON object the original instructions specified — " +
		"no prose, no markdown code fences, nothing before or after the JSON. " +
		"Preserve all findings, verdicts, and issues from your previous response; " +
		"just fit them into the required JSON fields.\n\n<previous_response>\n" +
		result.Output + "\n</previous_response>"

	fixed, ferr := runPrompt(fixPrompt, model, opts.MaxTokens)
	if ferr != nil {
		return fmt.Errorf("failed to parse agent %q output as JSON, and the reformat retry failed: %w\noriginal output:\n%s",
			opts.SkillName, ferr, result.Output)
	}

	if err := parseAgentJSON(fixed, dest); err != nil {
		return fmt.Errorf("failed to parse agent %q output as JSON even after a reformat retry: %w\noriginal output:\n%s\nreformat attempt:\n%s",
			opts.SkillName, err, result.Output, fixed)
	}
	return nil
}

// parseAgentJSON extracts the `{...}` object from raw agent output and
// unmarshals it into dest, falling back to repairJSON for stray unescaped
// quotes if the first attempt fails.
func parseAgentJSON(output string, dest any) error {
	firstBrace := strings.Index(output, "{")
	lastBrace := strings.LastIndex(output, "}")
	if firstBrace != -1 && lastBrace != -1 && lastBrace > firstBrace {
		output = output[firstBrace : lastBrace+1]
	} else {
		output = strings.TrimSpace(output)
	}

	if err := json.Unmarshal([]byte(output), dest); err != nil {
		// Common model mistake: literal, unescaped double quotes used for
		// inline quotation inside a string value (e.g. the model writes
		// prose like `the criterion "X" failed` inside a JSON string
		// without escaping it). Try to repair and re-parse before giving up.
		if repaired := repairJSON(output); repaired != output {
			if err2 := json.Unmarshal([]byte(repaired), dest); err2 == nil {
				return nil
			}
		}
		return err
	}
	return nil
}

// repairJSON fixes literal, unescaped double quotes inside JSON string
// values. It walks the text tracking whether it's inside a string, and
// only treats a `"` as a real string terminator when the next
// non-whitespace character is a valid JSON continuation (`,` `}` `]`
// `:`); otherwise the quote is escaped in place.
func repairJSON(s string) string {
	var b strings.Builder
	inString := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inString {
			if c == '\\' && i+1 < len(s) {
				b.WriteByte(c)
				b.WriteByte(s[i+1])
				i++
				continue
			}
			if c == '"' {
				if isJSONStringTerminator(s, i+1) {
					inString = false
					b.WriteByte(c)
				} else {
					b.WriteString(`\"`)
				}
				continue
			}
			b.WriteByte(c)
			continue
		}
		if c == '"' {
			inString = true
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isJSONStringTerminator(s string, i int) bool {
	for i < len(s) {
		switch s[i] {
		case ' ', '\t', '\n', '\r':
			i++
			continue
		}
		break
	}
	if i >= len(s) {
		return true
	}
	switch s[i] {
	case ',', '}', ']', ':':
		return true
	}
	return false
}
