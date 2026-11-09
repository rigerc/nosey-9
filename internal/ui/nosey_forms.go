package ui

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"github.com/rigerc/nosey-9/internal/catalog"
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/registry"
)

func (m *noseyModel) openForm(kind string, labels, values []string) {
	m.modal = "form"
	m.details = ""
	m.form = noseyForm{kind: kind, labels: labels}
	for i, value := range values {
		input := textinput.New()
		input.SetValue(value)
		input.CharLimit = 512
		input.Prompt = ""
		input.SetWidth(60)
		if i == 0 {
			input.Focus()
		}
		m.form.inputs = append(m.form.inputs, input)
	}
	m.configureFormChoices()
}
func (m noseyModel) updateModal(key tea.KeyPressMsg) (noseyModel, tea.Cmd) {
	if key.String() == "esc" {
		m.modal = ""
		m.err = nil
		return m, nil
	}
	if m.modal == "sort" {
		return m.updateSort(key)
	}
	if m.modal == "details" {
		m.scrollModal(key.String())
		return m, nil
	}
	if m.modal == "install" {
		return m.updateInstallPage(key.String())
	}
	switch key.String() {
	case "tab", "shift+tab":
		m.form.inputs[m.form.index].Blur()
		delta := 1
		if key.String() == "shift+tab" {
			delta = len(m.form.inputs) - 1
		}
		m.form.index = (m.form.index + delta) % len(m.form.inputs)
		m.form.inputs[m.form.index].Focus()
		return m, textinput.Blink
	case "enter", "ctrl+s":
		return m.submitForm()
	default:
		if len(m.form.choices[m.form.index]) > 0 {
			return m.updateChoiceField(key)
		}
		var cmd tea.Cmd
		m.form.inputs[m.form.index], cmd = m.form.inputs[m.form.index].Update(key)
		return m, cmd
	}
}
func (m *noseyModel) scrollModal(key string) {
	switch key {
	case "up", "k":
		m.modalOffset = max(0, m.modalOffset-1)
	case "down", "j":
		m.modalOffset++
	case "pgdown":
		m.modalOffset += m.viewport.Height()
	case "pgup":
		m.modalOffset = max(0, m.modalOffset-m.viewport.Height())
	}
}
func (m noseyModel) submitForm() (noseyModel, tea.Cmd) {
	values := []string{}
	for _, input := range m.form.inputs {
		values = append(values, input.Value())
	}
	m.err = nil
	switch m.form.kind {
	case "install-agents":
		agents := csv(values[0])
		if len(agents) == 0 {
			m.err = fmt.Errorf("select at least one target agent")
			return m, nil
		}
		return m.openInstallForAgents(agents)
	case "trust":
		if values[0] != m.session.Resolved.Fingerprint || values[0] == "" {
			m.err = fmt.Errorf("fingerprint does not match")
			return m, nil
		}
		if err := m.session.Store.Trust(values[0], false); err != nil {
			m.err = err
			return m, nil
		}
		m.modal = ""
		m.generation++
		m.loading = true
		return m, m.load()
	case "registry":
		refs, err := m.session.Store.UserRegistries()
		if err != nil {
			m.err = err
			return m, nil
		}
		index := slices.IndexFunc(refs, func(r domain.RegistryRef) bool { return r.ID == values[0] })
		if index < 0 {
			m.err = fmt.Errorf("only user-owned registry entries can be edited here")
			return m, nil
		}
		if values[3] == "REMOVE" {
			refs = slices.Delete(refs, index, index+1)
		} else {
			enabled, err := strconv.ParseBool(values[1])
			if err != nil {
				m.err = err
				return m, nil
			}
			priority, err := strconv.Atoi(values[2])
			if err != nil {
				m.err = err
				return m, nil
			}
			refs[index].Enabled = enabled
			refs[index].Priority = priority
			ttl, err := strconv.Atoi(values[6])
			if err != nil {
				m.err = err
				return m, nil
			}
			refs[index].TTLHours = ttl
			if refs[index].Source.Kind == "local" {
				refs[index].Source.Path = values[4]
			} else {
				if err := registry.ValidateGit(values[4], values[5]); err != nil {
					m.err = err
					return m, nil
				}
				refs[index].Source.URI, refs[index].Source.Ref = values[4], values[5]
			}
		}
		if err = m.session.Store.Registries(refs); err != nil {
			m.err = err
			return m, nil
		}
		m.modal = ""
		m.generation++
		m.loading = true
		return m, m.load()
	case "filters":
		f := m.filters[m.activeFilter()]
		f.Query = values[0]
		if m.tab == 0 {
			f.RecommendationTechnologies = csv(values[1])
		} else {
			f.Technologies = csv(values[1])
		}
		f.Registries = csv(values[2])
		f.Repositories = csv(values[3])
		f.Statuses = csv(values[4])
		f.Qualities = csv(values[5])
		var err error
		f.MinScore, err = parseNumber(values[6])
		if err != nil {
			m.err = err
			return m, nil
		}
		f.MinRepositoryAverage, err = parseNumber(values[7])
		if err != nil {
			m.err = err
			return m, nil
		}
		f.Priorities = csv(values[8])
		f.ScoreState = values[9]
		if !slices.Contains([]string{"any", "scored", "unscored"}, f.ScoreState) {
			m.err = fmt.Errorf("score state must be any, scored or unscored")
			return m, nil
		}
		if err := catalog.ValidateFilters(m.session.Catalog, f); err != nil {
			m.err = err
			return m, nil
		}
		m.filters[m.activeFilter()] = f
		m.modal = ""
	case "setting":
		key := m.form.labels[0]
		data := []byte(values[0])
		switch key {
		case "installation.agents":
			data, _ = json.Marshal(csv(values[0]))
		case "installation.method", "ui.mode":
			data, _ = json.Marshal(values[0])
		case "detection.exclude":
			data, _ = json.Marshal(csv(values[0]))
		case "ui.minQuality":
			data, _ = json.Marshal(values[0])
		case "ui.minScore", "ui.minRepoScore":
			score, convErr := strconv.Atoi(strings.TrimSpace(values[0]))
			if convErr != nil || score < 0 || score > 100 {
				m.err = fmt.Errorf("score must be a whole number from 0 to 100")
				return m, nil
			}
			values[0] = strconv.Itoa(score)
			data = []byte(values[0])
		}
		err := m.session.Store.Set(key, data, false)
		if err != nil {
			m.err = err
			return m, nil
		}
		if key == "ui.collapseRepos" {
			m.session.Resolved.Settings.UI.CollapseRepos = values[0] == "true"
			m.collapsed = map[string]bool{}
			m.modal = ""
			m.message = "Repository groups will start " + collapseLabel(values[0] == "true") + "."
			return m, nil
		}
		if key == "ui.minScore" || key == "ui.minRepoScore" || key == "ui.minQuality" {
			ui := &m.session.Resolved.Settings.UI
			switch key {
			case "ui.minScore":
				ui.MinScore, _ = strconv.Atoi(values[0])
			case "ui.minRepoScore":
				ui.MinRepoScore, _ = strconv.Atoi(values[0])
			default:
				ui.MinQuality = values[0]
			}
			m.modal = ""
			m.message = "Result thresholds saved."
			return m, nil
		}
		if key == "ui.mode" {
			m.session.Resolved.Settings.UI.Mode = values[0]
			m.modal = ""
			m.message = "Display mode saved; applies next launch."
			return m, nil
		}
		m.modal = ""
		m.generation++
		m.loading = true
		return m, m.load()
	case "add":
		path := values[1]
		source := domain.RegistrySource{Kind: "git", URI: path}
		if strings.HasPrefix(path, "/") {
			source = domain.RegistrySource{Kind: "local", Path: path}
		} else if err := registry.ValidateGit(path, ""); err != nil {
			m.err = err
			return m, nil
		}
		refs, err := m.session.Store.UserRegistries()
		if err != nil {
			m.err = err
			return m, nil
		}
		refs = append(refs, domain.RegistryRef{ID: values[0], Source: source, Enabled: true, Priority: 20})
		if err = m.session.Store.Registries(refs); err != nil {
			m.err = err
			return m, nil
		}
		m.modal = ""
		m.generation++
		m.loading = true
		return m, m.load()
	case "new":
		opts := registry.InitOptions{Path: values[0], RegistryID: values[1], Version: "0.1.0"}
		var err error
		opts.CI, err = strconv.ParseBool(values[2])
		if err != nil {
			m.err = err
			return m, nil
		}
		opts.Git, err = strconv.ParseBool(values[3])
		if err != nil {
			m.err = err
			return m, nil
		}
		opts.Register, err = strconv.ParseBool(values[4])
		if err != nil {
			m.err = err
			return m, nil
		}
		opts.GitHubRepo = strings.TrimSpace(values[5])
		opts.Public = values[6] == "public"
		if opts.GitHubRepo != "" && !registry.ValidGitHubRepo(opts.GitHubRepo) {
			m.err = fmt.Errorf("GitHub repository must be OWNER/NAME")
			return m, nil
		}
		m.modal = ""
		m.loading = true
		generation := m.generation
		return m, func() tea.Msg {
			r, err := m.session.Registry.Init(m.ctx, m.session.Catalog, opts)
			message := "Created registry " + r.Path
			if err == nil && opts.GitHubRepo != "" {
				message += " and published https://github.com/" + opts.GitHubRepo
			}
			return noseyOperation{message: message, err: err, generation: generation}
		}
	}
	return m, nil
}

// openNewRegistryForm opens the registry-creation form shared by the shortcut and the settings row.
func (m *noseyModel) openNewRegistryForm() {
	m.openForm("new", []string{"New absolute registry path", "Template registry ID", "GitHub CI: true or false", "Initialize Git: true or false", "Register for this user: true or false", "GitHub repository: OWNER/NAME or blank", "GitHub repository visibility"}, []string{"", "nosey:official", "false", "false", "false", "", "private"})
	m.details = "Creates an empty registry. A GitHub repository needs a Git identity and a GitHub token (GITHUB_TOKEN, GH_TOKEN or gh auth login)."
}
func (m noseyModel) setting(row noseyRow) (noseyModel, tea.Cmd) {
	m.settingsAnchor = row.id
	switch row.id {
	case "display":
		m.openDisplaySelect()
	case "collapse":
		m.openForm("setting", []string{"ui.collapseRepos"}, []string{strconv.FormatBool(m.session.Resolved.Settings.UI.CollapseRepos)})
	case "minscore":
		m.openForm("setting", []string{"ui.minScore"}, []string{strconv.Itoa(m.session.Resolved.Settings.UI.MinScore)})
	case "minrepo":
		m.openForm("setting", []string{"ui.minRepoScore"}, []string{strconv.Itoa(m.session.Resolved.Settings.UI.MinRepoScore)})
	case "minquality":
		m.openForm("setting", []string{"ui.minQuality"}, []string{m.session.Resolved.Settings.UI.MinQuality})
	case "agents":
		m.openForm("setting", []string{"installation.agents"}, []string{strings.Join(m.session.Resolved.Settings.Installation.Agents, ",")})
	case "method":
		m.openForm("setting", []string{"installation.method"}, []string{m.session.Resolved.Settings.Installation.Method})
	case "ttl":
		m.openForm("setting", []string{"updates.ttlHours"}, []string{strconv.Itoa(m.session.Resolved.Settings.Updates.TTL)})
	case "mcp":
		m.openForm("setting", []string{"detection.enableMcpPackage"}, []string{strconv.FormatBool(m.session.Resolved.Settings.Detection.MCP)})
	case "bytes":
		m.openForm("setting", []string{"detection.maxFileBytes"}, []string{strconv.FormatInt(m.session.Resolved.Settings.Detection.MaxBytes, 10)})
	case "exclude":
		m.openForm("setting", []string{"detection.exclude"}, []string{strings.Join(m.session.Resolved.Settings.Detection.Exclude, ",")})
	case "trust":
		m.openForm("trust", []string{"Type the full fingerprint to approve"}, []string{""})
		m.details = m.session.Resolved.Fingerprint
	case "revoke":
		err := m.session.Store.Trust("", true)
		m.err = err
		if err == nil {
			m.generation++
			return m, m.load()
		}
	case "new":
		m.openNewRegistryForm()
	case "add":
		m.openForm("add", []string{"Registry ID", "Absolute local path or Git URL"}, []string{"", ""})
	default:
		if row.kind == "pending" {
			m.details = "Workspace: " + m.cwd + "\nFingerprint: " + m.session.Resolved.Fingerprint
			m.modal = "details"
		} else {
			for _, ref := range m.session.Resolved.Settings.Registries {
				if ref.ID != row.id {
					continue
				}
				location := ref.Source.URI
				if ref.Source.Kind == "local" {
					location = ref.Source.Path
				}
				m.openForm("registry", []string{"ID", "Enabled: true or false", "Priority", "Remove: type REMOVE", "Source URL or absolute path", "Requested Git ref", "Refresh hours (0 inherits)"}, []string{ref.ID, strconv.FormatBool(ref.Enabled), strconv.Itoa(ref.Priority), "", location, ref.Source.Ref, strconv.Itoa(ref.TTLHours)})
			}
		}
	}
	return m, nil
}
func csv(value string) []string {
	result := []string{}
	for _, v := range strings.Split(value, ",") {
		if v = strings.TrimSpace(v); v != "" {
			result = append(result, v)
		}
	}
	slices.Sort(result)
	return slices.Compact(result)
}
func number(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}
func parseNumber(value string) (*float64, error) {
	if value == "" {
		return nil, nil
	}
	v, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 100 {
		return nil, fmt.Errorf("threshold must be 0..100")
	}
	return &v, nil
}
