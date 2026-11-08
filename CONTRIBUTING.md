# Contributing to nosey

Implement changes in the application source, add focused tests, and run:

```sh
go test ./...
make format-check
make build
```

`make check` also requires optional lint tooling. Keep Cobra `--help`
accurate. Update README only when essential user workflows or onboarding
change. Do not maintain template documentation, generate planning artifacts, or
publish unless the task explicitly requires it. Preserve third-party notices.
