package installation

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// Check records one concrete runtime compatibility probe.
type Check struct{ ID, Status, Code, Message string }

// CheckPrerequisites checks executable versions and the pinned installer without project writes.
func (s Service) CheckPrerequisites(ctx context.Context) []Check {
	checks := []Check{}
	for _, name := range []string{"git", "node", "npm", "npx", "skills"} {
		executable, args := name, []string{"--version"}
		if name == "skills" {
			executable, args = "npx", []string{"--yes", "skills@" + Version, "--version"}
		}
		out, err := s.run(ctx, s.Store.Workspace, s.env(), executable, args...)
		check := Check{ID: name, Status: "pass", Code: "OK", Message: strings.TrimSpace(string(out.Stdout))}
		switch {
		case err != nil:
			check.Status, check.Code, check.Message = "fail", "E_PREREQUISITE", name+" version probe failed"
		case name == "node" && !supportedNode(check.Message):
			check.Status, check.Code, check.Message = "fail", "E_NODE_VERSION", "skills@"+Version+" requires Node >=22.20.0"
		case name == "skills" && check.Message != Version:
			check.Status, check.Code, check.Message = "fail", "E_INSTALLER_VERSION", "expected skills@"+Version
		}
		checks = append(checks, check)
	}
	return checks
}
func supportedNode(version string) bool {
	parts := strings.Split(strings.TrimPrefix(version, "v"), ".")
	if len(parts) < 3 {
		return false
	}
	major, e1 := strconv.Atoi(parts[0])
	minor, e2 := strconv.Atoi(parts[1])
	patch, e3 := strconv.Atoi(parts[2])
	return e1 == nil && e2 == nil && e3 == nil && patch >= 0 && (major > 22 || major == 22 && minor >= 20)
}
func (s Service) requirePrerequisites(ctx context.Context) error {
	for _, check := range s.CheckPrerequisites(ctx) {
		if check.Status == "fail" {
			return PrerequisiteError{Reason: fmt.Sprintf("%s: %s", check.Code, check.Message)}
		}
	}
	return nil
}
