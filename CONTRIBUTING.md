# Contributing to nosey

Implement changes in the application source, add focused tests, and run:

```sh
go test ./...
make format-check
make build
```

Git is needed for test fixtures and hooks only; the binary must not invoke
`git` or `gh`. `make native-runtime-check` verifies that with a sanitized PATH.

`make check` also requires optional lint tooling. Keep Cobra `--help`
accurate. Update README only when essential user workflows or onboarding
change. Do not maintain template documentation, generate planning artifacts, or
publish unless the task explicitly requires it. Preserve third-party notices.
