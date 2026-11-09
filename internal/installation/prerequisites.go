package installation

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// Check records one concrete runtime compatibility probe.
type Check struct{ ID, Status, Code, Message string }

// CheckPrerequisites probes the executables Nosey needs without project writes. Git is the only one.
func (s Service) CheckPrerequisites(ctx context.Context) []Check {
	check := Check{ID: "git", Status: "pass", Code: "OK"}
	out, err := s.run(ctx, os.TempDir(), nil, "git", "--version")
	if err != nil {
		check.Status, check.Code, check.Message = "fail", "E_PREREQUISITE", "git version probe failed"
	} else {
		check.Message = strings.TrimSpace(string(out.Stdout))
	}
	return []Check{check}
}

func (s Service) requireGit(ctx context.Context) error {
	for _, check := range s.CheckPrerequisites(ctx) {
		if check.Status == "fail" {
			return PrerequisiteError{Reason: fmt.Sprintf("%s: %s", check.Code, check.Message)}
		}
	}
	return nil
}
