## Scope

Plan/task, base PR (if stacked), delivered behavior, and exclusions:

## Validation

- [ ] `go test -count=1 ./...`
- [ ] `go test -race -count=1 ./...`
- [ ] `go vet ./...` and `go build ./...`
- [ ] `gofmt -l .` prints nothing
- [ ] Native macOS and Windows CI pass
- [ ] Tests use temporary repositories and synthetic provider data

Record results and any unverified behavior. Do not claim future work is tested.

## Review

Record reviewer, reviewed revision, findings, and fixes. Label AI reviews and
record human approval separately. A review request is not a completed review.
This PR is not merged automatically.
