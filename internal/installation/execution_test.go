package installation

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/process"
	"github.com/rigerc/nosey-9/internal/storage"
)

// proofRunner models the pinned CLI's captured add/list/lock protocol, including failure cases.
type proofRunner struct {
	workspace, source, mode string
	calls                   int
}

func (r *proofRunner) Run(_ context.Context, _ string, _ []string, name string, args ...string) (process.Output, error) {
	r.calls++
	if slices.Contains(args, "--version") {
		v := "1.0.0"
		if name == "node" {
			v = "v22.20.0"
		}
		if slices.Contains(args, "skills@"+Version) {
			v = Version
		}
		return process.Output{Stdout: []byte(v)}, nil
	}
	if slices.Contains(args, "add") {
		if r.mode == "failure" {
			return process.Output{}, fmt.Errorf("fixture installer failure")
		}
		index := slices.Index(args, "add")
		r.source = args[index+1]
		if err := copyTree(r.source, filepath.Join(r.workspace, ".agents", "skills", "fixture")); err != nil {
			return process.Output{}, err
		}
		hash, err := upstreamHash(r.workspace, ".agents/skills/fixture")
		if err != nil {
			return process.Output{}, err
		}
		if err = storage.JSON(filepath.Join(r.workspace, "skills-lock.json"), projectLock{Version: 1, Skills: map[string]lockEntry{"fixture": {Source: r.source, Type: "local", Hash: hash}}}); err != nil {
			return process.Output{}, err
		}
		if r.mode == "malformed" {
			return process.Output{Stdout: []byte(`{`)}, nil
		}
		scope := "project"
		if r.mode == "global" {
			scope = "global"
		}
		return encodedProof([]upstreamItem{{Name: "fixture", Status: "installed", Source: r.source, Scope: scope, Agents: []string{"Codex", "Claude Code"}}})
	}
	if slices.Contains(args, "list") {
		if r.source == "" {
			return process.Output{Stdout: []byte(`[]`)}, nil
		}
		if r.mode == "unverified" {
			return process.Output{Stdout: []byte(`[]`)}, nil
		}
		return encodedProof([]upstreamItem{{Name: "fixture", Source: r.source, Scope: "project", Agents: []string{"Codex", "Claude Code"}, Path: filepath.Join(r.workspace, ".agents", "skills", "fixture")}})
	}
	return process.Output{}, fmt.Errorf("unexpected fixture invocation")
}
func encodedProof(items []upstreamItem) (process.Output, error) {
	data, err := json.Marshal(items)
	return process.Output{Stdout: data}, err
}

func TestExecutionRequiresPerSkillProjectProofAndReconcilesRetries(t *testing.T) {
	for _, mode := range []string{"success", "failure", "malformed", "global", "unverified"} {
		t.Run(mode, func(t *testing.T) {
			s, p, _ := localPlan(t)
			runner := &proofRunner{workspace: s.Store.Workspace, mode: mode}
			s.Runner = runner
			err := s.Execute(context.Background(), p, *p.Approval.Digest, true)
			if mode != "success" {
				if err == nil {
					t.Fatal("unproven install accepted", mode)
				}
				if len(s.readReceipts()) != 0 {
					t.Fatal("unproven install attributed")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(s.readReceipts()) != 1 {
				t.Fatal("verified origin not recorded")
			}
			c := domain.Catalog{Skills: map[string]domain.Skill{"test:fixture": {ID: "test:fixture", RegistryID: "test:registry", Source: domain.Source{Kind: "local", Path: "skill", SkillName: "fixture"}}}}
			result := s.Inventory(context.Background(), c)
			if !result.Complete || len(result.Skills) != 1 || result.Skills[0].Status != "installed" || result.Skills[0].SkillID == nil {
				t.Fatal(result)
			}
			if !s.diskInstalled(c)["test:fixture"] {
				t.Fatal("preview failed to corroborate installed conflict")
			}
			// A retry must not overwrite the existing installation.
			if err = s.Execute(context.Background(), p, *p.Approval.Digest, true); err != nil {
				t.Fatal("safe retry failed", err)
			}
			if err = storage.Write(filepath.Join(s.Store.Workspace, ".agents/skills/fixture/SKILL.md"), []byte("tampered")); err != nil {
				t.Fatal(err)
			}
			if s.diskInstalled(c)["test:fixture"] {
				t.Fatal("tampered installation accepted as proof")
			}
		})
	}
}
func TestNodeVersionRequirementAndUnknownInventory(t *testing.T) {
	for _, version := range []string{"v22.19.9", "v20.20.0", "vgarbage", "22.20"} {
		if supportedNode(version) {
			t.Fatal("unsupported Node accepted", version)
		}
	}
	for _, version := range []string{"v22.20.0", "v23.0.0", "v26.13.0"} {
		if !supportedNode(version) {
			t.Fatal("supported Node rejected", version)
		}
	}
	s, _, runner := localPlan(t)
	runner.err = fmt.Errorf("unavailable")
	inventory := s.Inventory(context.Background(), domain.Catalog{})
	if inventory.Complete || len(inventory.Diagnostics) == 0 {
		t.Fatal("failed inventory claimed complete")
	}
	if s.requirePrerequisites(context.Background()) == nil {
		t.Fatal("missing prerequisites accepted")
	}
	for _, data := range []string{"Body without frontmatter", "---\nname: x\n", "---\ndescription: Missing name\n---\n"} {
		if _, err := skillName([]byte(data)); err == nil {
			t.Fatal("invalid frontmatter accepted")
		}
	}
	root := t.TempDir()
	if err := os.Symlink(root, filepath.Join(root, "link")); err == nil {
		if _, err := treeHash(root); err == nil {
			t.Fatal("source symlinks accepted")
		}
	}
	if normalizeSource("https://github.com/Owner/Repo.git", "/") != "owner/repo" || !strings.HasSuffix(normalizeSource("./skill", s.Store.Workspace), "skill") {
		t.Fatal("logical source normalization failed")
	}
}
