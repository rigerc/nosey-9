package installation

import (
	"encoding/json"
	"strings"

	"github.com/rigerc/nosey-9/internal/storage"
)

// Digest hashes the approved semantic projection, excluding presentation/consent state.
func Digest(p Plan) (string, error) {
	bytes, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	var doc map[string]json.RawMessage
	if err = json.Unmarshal(bytes, &doc); err != nil {
		return "", err
	}
	delete(doc, "diagnostics")
	delete(doc, "blocked")
	var approval map[string]json.RawMessage
	if err = json.Unmarshal(doc["approval"], &approval); err != nil {
		return "", err
	}
	delete(approval, "digest")
	doc["approval"], err = json.Marshal(approval)
	if err != nil {
		return "", err
	}
	var items []map[string]json.RawMessage
	if err = json.Unmarshal(doc["items"], &items); err != nil {
		return "", err
	}
	for _, item := range items {
		delete(item, "status")
	}
	doc["items"], err = json.Marshal(items)
	if err != nil {
		return "", err
	}
	var value any
	if err = json.Unmarshal(mustJSON(doc), &value); err != nil {
		return "", err
	}
	var b strings.Builder
	encoder := json.NewEncoder(&b)
	encoder.SetEscapeHTML(false)
	if err = encoder.Encode(value); err != nil {
		return "", err
	}
	return storage.Hash([]byte(strings.TrimSuffix(b.String(), "\n"))), nil
}

// mustJSON handles only an already decoded JSON object; Marshal cannot fail for RawMessage values validated above.
func mustJSON(value map[string]json.RawMessage) []byte { data, _ := json.Marshal(value); return data }
