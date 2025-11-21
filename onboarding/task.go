package onboarding

import (
	"bufio"
	"errors"
	"fmt"
	"strings"
)

// Task represents a single checklist item on a GitHub onboarding issue.
type Task struct {
	Name     string
	Number   int
	Owner    string
	Complete bool
}

// GetProjectNameFromProjectTitle extracts the project name from an issue title in the format
// "[PROJECT ONBOARDING] <project name>".
func GetProjectNameFromProjectTitle(title string) (string, error) {
	const titlePrefix = "PROJECT ONBOARDING]"
	if title == "" {
		return "", errors.New("title cannot be empty")
	}

	parts := strings.Split(title, titlePrefix)
	if len(parts) < 2 {
		return "", fmt.Errorf("title %q does not contain the substring %s", title, titlePrefix)
	}
	return strings.TrimSpace(parts[1]), nil
}

// getOnboardingTasks parses the body of an onboarding issue and returns the tasks listed in the checklists.
//
//lint:ignore U1000 This function will be used in future implementation
func getOnboardingTasks(projectName, issueDescription string) []Task {
	var tasks []Task
	scanner := bufio.NewScanner(strings.NewReader(issueDescription))

	currentOwner := projectName
	currentTaskNumber := 1

	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case line == "Things that the CNCF will do or help the project to do:" || line == "**Things that the CNCF will do or help the project to do:**":
			currentOwner = "CNCF"
		case strings.HasPrefix(line, "- [x]"):
			taskName := strings.TrimSpace(strings.TrimPrefix(line, "- [x]"))
			tasks = append(tasks, Task{Number: currentTaskNumber, Owner: currentOwner, Complete: true, Name: taskName})
			currentTaskNumber++
		case strings.HasPrefix(line, "- [ ]"):
			taskName := strings.TrimSpace(strings.TrimPrefix(line, "- [ ]"))
			tasks = append(tasks, Task{Number: currentTaskNumber, Owner: currentOwner, Complete: false, Name: taskName})
			currentTaskNumber++
		}
	}
	return tasks
}
