package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func settingsCommand(o *cliOptions) *cobra.Command {
	root := &cobra.Command{Use: "config", Short: "Read or update schema-validated JSON settings"}
	root.AddCommand(&cobra.Command{Use: "get KEY", Args: cobra.ExactArgs(1), RunE: func(cmd *cobra.Command, args []string) error {
		store, err := o.store()
		if err != nil {
			return err
		}
		resolved, err := store.Resolve()
		if err != nil {
			return err
		}
		data, _ := json.Marshal(resolved.Settings)
		var doc map[string]json.RawMessage
		if err = json.Unmarshal(data, &doc); err != nil {
			return err
		}
		parts := strings.Split(args[0], ".")
		value, ok := doc[parts[0]]
		if len(parts) == 2 {
			var nested map[string]json.RawMessage
			if err = json.Unmarshal(value, &nested); err != nil {
				return err
			}
			value, ok = nested[parts[1]]
		}
		if !ok || len(parts) > 2 {
			return fmt.Errorf("unknown key")
		}
		cmd.Println(string(value))
		return nil
	}})
	project := false
	set := &cobra.Command{Use: "set KEY JSON_VALUE", Args: cobra.ExactArgs(2), RunE: func(_ *cobra.Command, args []string) error {
		store, err := o.store()
		if err != nil {
			return err
		}
		return store.Set(args[0], []byte(args[1]), project)
	}}
	set.Flags().BoolVar(&project, "project", false, "write this workspace's settings layer")
	root.AddCommand(set)
	return root
}
