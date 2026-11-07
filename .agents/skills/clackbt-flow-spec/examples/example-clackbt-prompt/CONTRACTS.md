# Contracts

`clackbt.json` binds each canonical prompt screen to `TextModel` / `ConfirmModel` and result struct fields. `contracts.json` defines event origins, render nodes, state fields, and the output command. `screens.json` and `machine.json` own the user-visible flow and process exit codes.

Source-derived part: `cmd/go-cli-template/prompt.go` in the inspected repository. Extra failure/interrupt states are proposed for design coverage; they are not claims about implemented behavior.
