# Development and Review Workflow

Use Go 1.26.5 and Git 2.36 or newer. From the repository root:

```text
go test -count=1 ./...
go test -race -count=1 ./...
go vet ./...
go build ./...
gofmt -l .
```

`gofmt -l .` must print nothing; fix formatting with `gofmt -w <files>`.
`make verify` runs the same checks. Windows contributors can use the Go
commands directly in PowerShell. CI runs both native macOS and Windows tests.
For a quick iteration, use `go test -count=1 ./internal/<package>`.

## Test isolation

`internal/testutil` provides real temporary Git repositories, a controllable
clock, and an operation recorder. Repository helpers isolate child Git
configuration, hooks, credentials, and home directories from the developer.
`Repository.Git` accepts trusted test code, not untrusted commands; keep all
path operands inside temporary fixtures. It is not a Git command sandbox.
Use synthetic provider records and injected failures; never scan or remove
real user worktrees or sessions in tests.

## Delivery

Follow the [implementation plans](plans/README.md) in order. Each stage, or
clearly labeled reviewable slice, gets a PR with its tests and limitations.
Request independent review, fix blocking findings, and keep human approval
separate from AI review. Do not merge automatically. A dependent PR may be
stacked on its prerequisite; name the base PR and do not claim it is merged.

The baseline deliberately uses standard Go tools, not a custom verifier,
acceptance manifest, or stage activation system. Add product tests alongside
the behavior they protect; passing baseline tests is not evidence that
unimplemented cleanup behavior is safe.
