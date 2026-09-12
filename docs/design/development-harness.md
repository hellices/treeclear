# Minimal Development Checks

## Scope decision

The original development-harness design grew into a separate verification
framework. User feedback prioritized building Treeclear instead. The custom
runner, acceptance manifest, stage detector, and documentation checker are
removed rather than expanded or repaired.

Retain only:

- A pinned Go module and conventional `gofmt`, `go vet`, `go test`, race-test,
  and build commands.
- Native macOS and Windows CI using those same commands.
- Isolated temporary Git fixtures, a test clock, and an operation recorder.
- Short contributor guidance and a PR review checklist.

Product safety is enforced by behavior tests added with the corresponding
implementation, not by a second framework that interprets Go test output.
Native CI results and independent PR review remain required; AI review does
not represent human approval. See the [workflow](../development.md).
