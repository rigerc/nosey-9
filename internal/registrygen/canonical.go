package registrygen

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// document is one parsed data document in canonical form.
type document struct {
	role       string
	version    int
	versionRaw json.RawMessage
	entities   []json.RawMessage
	canonical  []byte
}

func (d *document) count() int { return len(d.entities) }

// canonicalize parses a data document, sorts top-level entities by id, and
// re-indents the result without reordering entity fields or nested arrays.
func canonicalize(role string, data []byte) (*document, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	token, err := dec.Token()
	if err != nil {
		return nil, inputError("%s: %w", role, err)
	}
	delim, ok := token.(json.Delim)
	if !ok || delim != '{' {
		return nil, inputError("%s: document must be a JSON object", role)
	}
	var versionRaw, arrayRaw json.RawMessage
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return nil, inputError("%s: %w", role, err)
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, inputError("%s: invalid object key", role)
		}
		var raw json.RawMessage
		if err = dec.Decode(&raw); err != nil {
			return nil, inputError("%s: %w", role, err)
		}
		switch key {
		case "schemaVersion":
			versionRaw = raw
		case role:
			arrayRaw = raw
		}
	}
	if _, err = dec.Token(); err != nil {
		return nil, inputError("%s: %w", role, err)
	}
	if len(versionRaw) == 0 || len(arrayRaw) == 0 {
		return nil, inputError("%s: document requires schemaVersion and %s", role, role)
	}
	var version int
	if err = json.Unmarshal(versionRaw, &version); err != nil {
		return nil, inputError("%s: schemaVersion must be an integer", role)
	}
	var compact bytes.Buffer
	if err = json.Compact(&compact, versionRaw); err != nil {
		return nil, inputError("%s: invalid schemaVersion", role)
	}
	versionRaw = append([]byte(nil), compact.Bytes()...)
	var entities []json.RawMessage
	if err = json.Unmarshal(arrayRaw, &entities); err != nil {
		return nil, inputError("%s: %s must be an array", role, role)
	}
	seen := make(map[string]bool, len(entities))
	ids := make([]string, len(entities))
	for i, entity := range entities {
		var header struct {
			ID string `json:"id"`
		}
		if err = json.Unmarshal(entity, &header); err != nil {
			return nil, inputError("%s: entity %d: %w", role, i, err)
		}
		if header.ID == "" {
			return nil, inputError("%s: entity %d has no id", role, i)
		}
		if seen[header.ID] {
			return nil, inputError("%s: duplicate id %q", role, header.ID)
		}
		seen[header.ID] = true
		ids[i] = header.ID
	}
	order := make([]int, len(entities))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return ids[order[a]] < ids[order[b]] })
	sorted := make([]json.RawMessage, len(entities))
	for i, index := range order {
		sorted[i] = entities[index]
	}
	doc := &document{role: role, version: version, versionRaw: versionRaw, entities: sorted}
	doc.canonical = doc.encode()
	return doc, nil
}

// encode renders the canonical document: UTF-8, LF, two-space indent,
// trailing newline, entities sorted by id, field order preserved.
func (d *document) encode() []byte {
	var b strings.Builder
	b.WriteString("{\n  \"schemaVersion\": ")
	b.Write(d.versionRaw)
	b.WriteString(",\n  ")
	b.WriteString(strconv.Quote(d.role))
	b.WriteString(": [")
	for i, entity := range d.entities {
		if i > 0 {
			b.WriteByte(',')
		}
		var indented bytes.Buffer
		if err := json.Indent(&indented, entity, "    ", "  "); err != nil {
			// Entities were decoded from valid JSON above; preserve raw bytes.
			indented.Reset()
			indented.Write(entity)
		}
		b.WriteString("\n    ")
		b.Write(indented.Bytes())
	}
	if len(d.entities) > 0 {
		b.WriteString("\n  ")
	}
	b.WriteString("]\n}\n")
	return []byte(b.String())
}

// documents returns canonical documents keyed by role.
type documents map[string]*document

func (d documents) counts() (skills, technologies, mappings int) {
	if doc := d["skills"]; doc != nil {
		skills = doc.count()
	}
	if doc := d["technologies"]; doc != nil {
		technologies = doc.count()
	}
	if doc := d["mappings"]; doc != nil {
		mappings = doc.count()
	}
	return skills, technologies, mappings
}

// schemaFor returns the runtime schema that validates one role's document.
func schemaFor(role string, version int) (string, error) {
	switch role {
	case "technologies":
		switch version {
		case 1:
			return "technologies.schema.json", nil
		case 2:
			return "technologies.v2.schema.json", nil
		default:
			return "", fmt.Errorf("unsupported technology schemaVersion %d", version)
		}
	case "skills":
		return "skills.effective.schema.json", nil
	case "mappings":
		return "mappings.effective.schema.json", nil
	case "repositories":
		return "repositories.schema.json", nil
	default:
		return "", fmt.Errorf("unknown data role %q", role)
	}
}
