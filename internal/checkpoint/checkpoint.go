package checkpoint

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Gate blocks until the human approves or rejects.
// Returns true for approval, false for rejection.
func Gate(plan string) bool {
	fmt.Println("\n" + strings.Repeat("=", 80))
	fmt.Println("  HUMAN CHECKPOINT — Step 3 of 9")
	fmt.Println(strings.Repeat("=", 80))
	fmt.Println("\nTech Lead plan for review:\n")
	fmt.Println(plan)
	fmt.Println("\n" + strings.Repeat("-", 80))
	fmt.Print("Approve this plan? [y/N/edit]: ")

	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		switch answer {
		case "y", "yes":
			fmt.Println("Plan approved. Continuing pipeline...")
			return true
		case "n", "no", "":
			fmt.Println("Plan rejected. Re-run after updating issues.")
			return false
		case "edit":
			fmt.Println("Open the state.json file, edit task_groups[].plan, then re-run with --resume.")
			return false
		default:
			fmt.Print("Enter y (approve), n (reject), or edit: ")
		}
	}
	return false
}
