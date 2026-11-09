package installation

import (
	"github.com/rigerc/nosey-9/internal/skillkit"
)

// agentTable returns the built-in agents plus the custom agents from user settings.
func (s Service) agentTable() (skillkit.AgentTable, error) {
	custom := make([]skillkit.CustomAgent, 0, len(s.Settings.Installation.CustomAgents))
	for _, a := range s.Settings.Installation.CustomAgents {
		custom = append(custom, skillkit.CustomAgent{ID: a.ID, Name: a.Name, SkillsDir: a.SkillsDir})
	}
	table, err := skillkit.NewAgentTable(s.home(), custom)
	if err != nil {
		return skillkit.AgentTable{}, PolicyError{Reason: "invalid custom agent: " + err.Error()}
	}
	return table, nil
}

// AgentNames returns every supported agent ID with its display name, custom agents included.
// An invalid custom agent set falls back to the built-in agents.
func (s Service) AgentNames() map[string]string {
	table, err := s.agentTable()
	if err != nil {
		table, _ = skillkit.NewAgentTable(s.home(), nil)
	}
	names := map[string]string{}
	for _, a := range table.All() {
		names[a.ID] = a.Name
	}
	return names
}
