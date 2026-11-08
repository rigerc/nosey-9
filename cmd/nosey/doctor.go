package main

import (
	"github.com/rigerc/nosey-9/internal/domain"
	"github.com/rigerc/nosey-9/internal/installation"
	"github.com/rigerc/nosey-9/internal/storage"
	"github.com/spf13/cobra"
)

type doctorCheck struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Code    string `json:"code"`
	Message string `json:"message"`
}
type doctorResult struct {
	Version     int                 `json:"outputVersion"`
	Diagnostics []domain.Diagnostic `json:"diagnostics"`
	Workspace   string              `json:"workspace"`
	Checks      []doctorCheck       `json:"checks"`
}

func doctorCommand(o *cliOptions) *cobra.Command {
	jsonMode := false
	cmd := &cobra.Command{Use: "doctor", Short: "Inspect settings, registry and pinned installer prerequisites", Args: cobra.NoArgs}
	cmd.Flags().BoolVar(&jsonMode, "json", false, "emit doctor JSON v1")
	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		store, err := o.store()
		if err != nil {
			return err
		}
		result := doctorResult{Version: 1, Workspace: store.Workspace, Diagnostics: []domain.Diagnostic{}, Checks: []doctorCheck{}}
		resolved, err := store.Resolve()
		if err != nil {
			result.Checks = append(result.Checks, doctorCheck{"settings", "fail", "E_SETTINGS", storage.Safe(err.Error())})
		} else {
			result.Checks = append(result.Checks, doctorCheck{"settings", "pass", "OK", "JSON settings validated"})
			for _, a := range resolved.Settings.Detection.Adapters {
				result.Checks = append(result.Checks, doctorCheck{"adapter:" + a.ID, "warning", "ADAPTER", a.Version + " sha256=" + a.Hash})
			}
		}
		s, configErr := o.configuredSession(cmd.Context())
		if configErr == nil {
			for _, check := range s.Installer.CheckPrerequisites(cmd.Context()) {
				result.Checks = append(result.Checks, doctorCheck{check.ID, check.Status, check.Code, storage.Safe(check.Message)})
			}
		}
		failed := false
		for _, check := range result.Checks {
			if check.Status == "fail" {
				failed = true
			}
		}
		if jsonMode {
			if err = emit(cmd, "doctor-output.schema.json", result); err != nil {
				return err
			}
		} else {
			for _, c := range result.Checks {
				cmd.Printf("%s %s: %s\n", c.Status, c.ID, c.Message)
			}
		}
		if failed {
			return installation.PrerequisiteError{Reason: "doctor found failed prerequisite checks"}
		}
		return nil
	}
	return cmd
}
