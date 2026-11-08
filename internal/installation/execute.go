package installation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/storage"
)

// Execute enforces reviewed consent and verifies individual project install outcomes.
func (s Service) Execute(ctx context.Context, p Plan, approval string, confirmed bool) error {
	if !confirmed {
		return PolicyError{Reason: "explicit confirmation required"}
	}
	if p.Blocked || p.Approval.Digest == nil {
		return PolicyError{Reason: "installation plan is blocked"}
	}
	digest, err := Digest(p)
	if err != nil {
		return err
	}
	if p.Approval.Required && approval != digest {
		return PolicyError{Reason: "additional approval requires --approve-plan " + digest + " with --yes"}
	}
	projectRoot, err := os.OpenRoot(s.Store.Workspace)
	if err != nil {
		return err
	}
	defer func() { _ = projectRoot.Close() }()
	if err = projectRoot.MkdirAll(".nosey", 0o700); err != nil {
		return err
	}
	release, err := acquireProjectLock(projectRoot)
	if err != nil {
		return err
	}
	defer release()
	for _, f := range p.Approval.Registries {
		root, ok := p.registryRoots[f.ID]
		if !ok {
			return PolicyError{Reason: "registry " + f.ID + " cannot be re-verified; regenerate the plan"}
		}
		hash, err := treeHash(root)
		if err != nil || hash != f.Hash {
			return PolicyError{Reason: "registry content changed since review"}
		}
	}
	for dst, src := range p.trees {
		expected := filepath.Base(dst)
		actual, hashErr := treeHash(src)
		if hashErr != nil || actual != expected {
			return fmt.Errorf("reviewed source changed")
		}
		if _, statErr := os.Stat(dst); errors.Is(statErr, os.ErrNotExist) {
			sourceLock, lockErr := storage.Acquire(ctx, dst+".lock")
			if lockErr != nil {
				return lockErr
			}
			if _, statErr = os.Stat(dst); errors.Is(statErr, os.ErrNotExist) {
				err = copyTree(src, dst)
			}
			sourceLock.Close()
			if err != nil {
				return err
			}
		}
		actual, hashErr = treeHash(dst)
		if hashErr != nil || actual != expected {
			return fmt.Errorf("persistent installation source integrity failure")
		}
	}
	paths := []string{".agents/skills", "skills-lock.json"}
	for _, item := range p.Items {
		for _, agent := range item.Agents {
			paths = append(paths, agentPaths[agent], filepath.Join(agentPaths[agent], item.SkillName))
		}
	}
	for _, path := range unique(paths) {
		if _, err = projectRoot.Stat(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return PolicyError{Reason: "installation path escapes project or is inaccessible: " + path}
		}
	}
	if err = s.requirePrerequisites(ctx); err != nil {
		return err
	}
	records, _ := s.read(ctx)
	locked, _ := s.readLock()
	done := []string{}
	fail := func(err error) error {
		if len(done) == 0 {
			return err
		}
		return fmt.Errorf("%w (already installed and verified: %s)", err, strings.Join(done, ", "))
	}
	for i, cmd := range p.Commands {
		item := p.Items[i]
		if s.alreadyInstalled(item, records, locked) {
			if err = s.receipt(item); err != nil {
				return fail(err)
			}
			done = append(done, item.SkillName)
			continue
		}
		out, runErr := s.run(ctx, p.Workspace, s.env(), cmd.Argv[0], cmd.Argv[1:]...)
		if runErr != nil {
			return fail(fmt.Errorf("installation failed for %s: %w", item.SkillName, runErr))
		}
		var installed []upstreamItem
		if err = json.Unmarshal(out.Stdout, &installed); err != nil {
			return fail(fmt.Errorf("invalid upstream installation result: %w", err))
		}
		matched := false
		for _, record := range installed {
			if record.Name == item.SkillName && record.Status == "installed" && record.Scope == "project" && agentEvidence(record.Agents, item.Agents) && s.sourceMatchesItem(record, item.Source) && (item.Revision == nil || record.Ref != nil && *record.Ref == *item.Revision || filepath.IsAbs(item.Source)) {
				matched = true
			}
		}
		if !matched {
			return fail(fmt.Errorf("per-skill installation success not established: %s", item.SkillName))
		}
		// Re-read once per install: the reconciliation below and the next item's skip check share this snapshot.
		if records, err = s.read(ctx); err != nil {
			return fail(err)
		}
		if locked, err = s.readLock(); err != nil {
			return fail(err)
		}
		verified := false
		for _, record := range records {
			if record.Name == item.SkillName && record.Scope == "project" && agentEvidence(record.Agents, item.Agents) && s.verifiedRecord(record, locked) && locked.Skills[record.Name].method() == item.Method && s.sourceMatchesItem(record, item.Source) {
				verified = true
			}
		}
		if !verified {
			return fail(fmt.Errorf("post-install verification unknown: %s", item.SkillName))
		}
		if err = s.receipt(item); err != nil {
			return fail(err)
		}
		done = append(done, item.SkillName)
	}
	return nil
}

// acquireProjectLock takes the project operation lock, reclaiming one left behind by a crashed run.
func acquireProjectLock(root *os.Root) (func(), error) {
	const name = ".nosey/operation.lock"
	for attempt := 0; ; attempt++ {
		f, err := root.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(f, "pid=%d\nstarted=%s\n", os.Getpid(), time.Now().UTC().Format(time.RFC3339))
			_ = f.Close()
			return func() { _ = root.Remove(name) }, nil
		}
		info, statErr := root.Lstat(name)
		if attempt == 0 && errors.Is(err, os.ErrExist) && statErr == nil && info.Mode().IsRegular() && time.Since(info.ModTime()) > staleLockAge {
			if rmErr := root.Remove(name); rmErr == nil || errors.Is(rmErr, os.ErrNotExist) {
				continue
			}
		}
		return nil, fmt.Errorf("another project operation is active or its lock path is unsafe (stale locks older than %s are reclaimed automatically; otherwise remove %s): %w", staleLockAge, name, err)
	}
}

type receipt struct {
	SkillID       *string  `json:"skillId"`
	Name          string   `json:"frontmatterName"`
	LogicalSource string   `json:"logicalSource"`
	RegistryID    *string  `json:"sourceRegistryId"`
	Path          *string  `json:"relativeSkillPath"`
	Ref           *string  `json:"requestedRef"`
	Revision      *string  `json:"resolvedRevision"`
	Agents        []string `json:"agents"`
	Method        string   `json:"method"`
	Provenance    string   `json:"provenance"`
	Upstream      string   `json:"upstreamSourceAtInstall"`
	At            string   `json:"installedAt"`
}

func (s Service) receipt(item Item) error {
	path := ".nosey/install-origins.json"
	doc := struct {
		Version int       `json:"schemaVersion"`
		Origins []receipt `json:"origins"`
	}{1, []receipt{}}
	// #nosec G304 -- this path is derived from the canonical workspace or application-owned cache.
	data, err := storage.Read(s.Store.Workspace, path, 4<<20)
	if err == nil {
		if err = json.Unmarshal(data, &doc); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	r := receipt{item.SkillID, item.SkillName, item.LogicalSource, item.RegistryID, nil, item.Ref, item.Revision, item.Agents, item.Method, item.Provenance, item.Source, time.Now().UTC().Format(time.RFC3339)}
	idx := slices.IndexFunc(doc.Origins, func(v receipt) bool { return v.LogicalSource == r.LogicalSource && v.Name == r.Name })
	if idx < 0 {
		doc.Origins = append(doc.Origins, r)
	} else {
		doc.Origins[idx] = r
	}
	bytes, err := contracts.Encode("install-origins.schema.json", doc)
	if err != nil {
		return err
	}
	return storage.WriteAt(s.Store.Workspace, path, bytes)
}
