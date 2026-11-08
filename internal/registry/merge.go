package registry

import (
	"encoding/json"
	"fmt"
	"slices"

	"github.com/rigerc/nosey-9/internal/domain"
)

func mergeEntity(base, overlay domain.RawEntity, kind string, provenance map[string]string, registry string) error {
	for key, value := range overlay {
		if (key == "rules" && kind == "technologies") || (key == "recommendations" && kind == "mappings") {
			var old, incoming []domain.RawEntity
			_ = json.Unmarshal(base[key], &old)
			if err := json.Unmarshal(value, &incoming); err != nil {
				return err
			}
			if key == "recommendations" && len(incoming) == 0 {
				base[key] = value
				provenance[key] = registry
				continue
			}
			childKey := "id"
			if key == "recommendations" {
				childKey = "skillId"
			}
			for _, child := range incoming {
				var id string
				_ = json.Unmarshal(child[childKey], &id)
				for field := range child {
					provenance[key+"["+id+"]."+field] = registry
				}
				idx := slices.IndexFunc(old, func(e domain.RawEntity) bool { return string(e[childKey]) == string(child[childKey]) })
				if idx < 0 {
					old = append(old, child)
				} else {
					if key == "rules" {
						if a, b := old[idx]["kind"], child["kind"]; len(a) > 0 && len(b) > 0 && string(a) != string(b) {
							return fmt.Errorf("rule %s kind changed in registry %s (base provenance %s)", id, registry, provenance[key])
						}
						for _, pair := range [][2]string{{"glob", "pathRegex"}, {"name", "nameRegex"}, {"key", "keyRegex"}} {
							if (len(old[idx][pair[0]]) > 0 && len(child[pair[1]]) > 0) || (len(old[idx][pair[1]]) > 0 && len(child[pair[0]]) > 0) {
								return fmt.Errorf("rule %s matcher form changed in %s", id, registry)
							}
						}
					}
					for k, v := range child {
						old[idx][k] = v
					}
				}
			}
			data, _ := json.Marshal(old)
			base[key] = data
		} else {
			base[key] = value
		}
		provenance[key] = registry
	}
	return nil
}
