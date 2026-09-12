## Stage and scope

- Stage and implementation-plan link:
- Base PR or prerequisite:
- Implemented requirements and intentionally excluded work:

## Validation

- [ ] `go run ./tools/harness verify` passes on the current revision.
- [ ] Both native macOS and Windows checks pass; cross-builds are not native tests.
- [ ] Required acceptance tests ran without skips and the cumulative stage is correct.
- [ ] New behavior has a recorded failing test before its implementation.
- [ ] Tests use temporary repositories and synthetic provider data only.

Record exact commands, platform, outcomes, and any test limitations here.

## Unverified requirements

List future-stage requirements and any evidence that is still missing. A
passing harness is not a claim that product cleanup is safe.

## Independent review

Record reviewer identity/type, reviewed commit range, findings, fixes, and
re-review results. Label AI review explicitly. A requested or pending review
is not completed review; resolve blocking findings before the next stage.

## Human approval

Record actual human approval separately, or state that it is pending. Do not
represent AI review, the PR author's own review, or a review request as human
approval. This PR is not merged automatically.
