package registrygen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/rigerc/nosey-9/internal/contracts"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/storage"
)

const (
	diagnosticSchema    = "E_STAGE_SCHEMA"
	diagnosticReference = "E_STAGE_REFERENCE"
	diagnosticManifest  = "E_STAGE_MANIFEST"
)

// Staged is one in-memory generation. Files holds the canonical bytes of every
// data document, the manifest and the lock, keyed by registry-relative path.
type Staged struct {
	Config      Config
	Files       map[string][]byte
	Summary     Summary
	Diagnostics []domain.Diagnostic
}

// Stage canonicalizes the registry with overrides replacing the named data
// roles. It never writes, and unlike Run it reports every entity-level problem
// instead of stopping at the first. An error means a document cannot be parsed.
func Stage(root string, overrides map[string][]byte) (Staged, error) {
	staged := Staged{Files: map[string][]byte{}, Diagnostics: []domain.Diagnostic{}}
	target, err := filepath.Abs(root)
	if err != nil {
		return staged, err
	}
	cfg, err := Load(target)
	if err != nil {
		return staged, err
	}
	staged.Config = cfg
	old, _, err := readLock(target)
	if err != nil {
		return staged, err
	}
	docs := documents{}
	for _, role := range sortedRoles(cfg.Files) {
		data, ok := overrides[role]
		if !ok {
			if data, err = storage.Read(target, cfg.Files[role], 16<<20); err != nil {
				return staged, err
			}
		}
		doc, canonicalErr := canonicalize(role, data)
		if canonicalErr != nil {
			return staged, canonicalErr
		}
		docs[role] = doc
		staged.Files[cfg.Files[role]] = doc.canonical
	}
	manifest, err := buildManifest(cfg)
	if err != nil {
		return staged, err
	}
	if err = stageEntities(docs, &staged); err != nil {
		return staged, err
	}
	problems, err := referenceProblems(docs)
	if err != nil {
		return staged, err
	}
	for _, problem := range problems {
		staged.Diagnostics = append(staged.Diagnostics, domain.Diagnostic{Code: diagnosticReference, Severity: "error", Message: problem.message, Subject: problem.subject})
	}
	if err = validateManifest(cfg.Manifest, manifest); err != nil {
		staged.Diagnostics = append(staged.Diagnostics, domain.Diagnostic{Code: diagnosticManifest, Severity: "error", Message: err.Error()})
	}
	if err = validateTemplates(target, cfg.Templates); err != nil {
		staged.Diagnostics = append(staged.Diagnostics, domain.Diagnostic{Code: diagnosticManifest, Severity: "error", Message: err.Error()})
	}
	lock := buildLock(cfg, docs, manifest, old, nil)
	lockBytes, err := lock.encode()
	if err != nil {
		return staged, err
	}
	staged.Files[cfg.Manifest] = manifest
	staged.Files[LockName] = lockBytes
	staged.Summary = lock.Summary
	return staged, nil
}

// stageEntities validates each entity alone so a failure names its entity
// rather than an array index, and one bad entity cannot hide the next.
func stageEntities(docs documents, staged *Staged) error {
	for _, role := range sortedDocumentRoles(docs) {
		doc := docs[role]
		schema, err := schemaFor(role, doc.version)
		if err != nil {
			return inputError("%s: %w", role, err)
		}
		prefix := "/" + role + "/0"
		for _, entity := range doc.entities {
			single := document{role: role, version: doc.version, versionRaw: doc.versionRaw, entities: doc.entities[:0:0]}
			single.entities = append(single.entities, entity)
			if err = contracts.Validate(schema, single.encode()); err != nil {
				staged.Diagnostics = append(staged.Diagnostics, domain.Diagnostic{
					Code:     diagnosticSchema,
					Severity: "error",
					Message:  strings.ReplaceAll(err.Error(), prefix, ""),
					Subject:  entityID(entity),
				})
			}
		}
	}
	return nil
}

func sortedDocumentRoles(docs documents) []string {
	roles := make(map[string]string, len(docs))
	for role := range docs {
		roles[role] = role
	}
	return sortedRoles(roles)
}

func entityID(entity json.RawMessage) string {
	decoded := idSet(&document{entities: []json.RawMessage{entity}})
	for id := range decoded {
		return id
	}
	return fmt.Sprintf("entity %s", bytes.TrimSpace(entity[:min(len(entity), 40)]))
}
