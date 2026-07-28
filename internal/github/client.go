package github

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// gh wraps the `gh` CLI. All GitHub operations go through it — no direct API calls.

func ListOpenIssues(repo string) ([]map[string]any, error) {
	out, err := gh("issue", "list", "--repo", repo, "--state", "open", "--json",
		"number,title,body,labels", "--limit", "100")
	if err != nil {
		return nil, err
	}
	var issues []map[string]any
	return issues, json.Unmarshal([]byte(out), &issues)
}

func CreateIssue(repo, title, body, issueType, priority string) (int, error) {
	labels := fmt.Sprintf("type:%s,priority:%s,followthesun", issueType, priority)
	out, err := gh("issue", "create",
		"--repo", repo,
		"--title", title,
		"--body", body,
		"--label", labels,
	)
	if err != nil {
		return 0, err
	}
	// `gh issue create` has no --json flag; it prints the issue URL on
	// success (e.g. https://github.com/owner/repo/issues/42).
	num, _, err := parseNumberFromURL(out)
	return num, err
}

func CreatePR(repo, branch, base, title, body string) (int, string, error) {
	out, err := gh("pr", "create",
		"--repo", repo,
		"--head", branch,
		"--base", base,
		"--title", title,
		"--body", body,
	)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") {
			// A resumed run (or a stray manual PR) may have already opened
			// a PR for this branch — reuse it and sync it to the intended
			// title/body rather than leaving stale content in place.
			num, url, findErr := existingPR(repo, branch)
			if findErr != nil {
				return 0, "", findErr
			}
			if _, editErr := gh("pr", "edit", strconv.Itoa(num),
				"--repo", repo, "--title", title, "--body", body); editErr != nil {
				return 0, "", editErr
			}
			return num, url, nil
		}
		return 0, "", err
	}
	// `gh pr create` has no --json flag either; it prints the PR URL on
	// success (e.g. https://github.com/owner/repo/pull/5).
	num, url, err := parseNumberFromURL(out)
	return num, url, err
}

func existingPR(repo, branch string) (int, string, error) {
	out, err := gh("pr", "view", branch, "--repo", repo, "--json", "number,url")
	if err != nil {
		return 0, "", err
	}
	var result struct {
		Number int    `json:"number"`
		URL    string `json:"url"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return 0, "", err
	}
	return result.Number, result.URL, nil
}

// parseNumberFromURL extracts the trailing numeric ID from a `gh ... create`
// URL, which is printed as the last line of stdout on success.
func parseNumberFromURL(out string) (int, string, error) {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	url := strings.TrimSpace(lines[len(lines)-1])
	parts := strings.Split(url, "/")
	num, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil {
		return 0, url, fmt.Errorf("could not parse number from url %q: %w", url, err)
	}
	return num, url, nil
}

func MergePR(repo string, prNumber int) error {
	_, err := gh("pr", "merge",
		"--repo", repo,
		fmt.Sprintf("%d", prNumber),
		"--squash",
		"--auto",
		"--delete-branch",
	)
	return err
}

func GetPRStatus(repo string, prNumber int) (string, error) {
	out, err := gh("pr", "view",
		"--repo", repo,
		fmt.Sprintf("%d", prNumber),
		"--json", "state,mergeable",
	)
	if err != nil {
		return "", err
	}
	var result struct {
		State     string
		Mergeable string
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		return "", err
	}
	return result.State, nil
}

func gh(args ...string) (string, error) {
	cmd := exec.Command("gh", args...)
	var out, errBuf bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errBuf

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh %s: %w\n%s", strings.Join(args, " "), err, errBuf.String())
	}
	return strings.TrimSpace(out.String()), nil
}
