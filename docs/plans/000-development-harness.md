# Minimal Development Baseline

- Sequence: 000, before the product plans
- Scope: Go tooling, native CI, and isolated test fixtures only
- Design: [Minimal Development Checks](../design/development-harness.md)

## Delivered baseline

- [x] Pin Go 1.26.5 and add module metadata and Apache-2.0 license.
- [x] Add ordinary Go test, race-test, vet, formatting, and build commands.
- [x] Run those commands in native macOS and Windows CI.
- [x] Provide temporary Git repositories, a clock, and operation recording.
- [x] Document focused PRs, independent review, and test isolation.

The earlier custom verification framework is intentionally removed following
user feedback. There is no acceptance manifest, source-stage detector, custom
subprocess runner, or documentation-link gate to maintain. New tests belong
with actual product behavior.

This baseline does not implement or certify safe cleanup. Continue with
[001 Safety Core](001-treeclear-core.md); use [the development workflow](../development.md)
for validation and review evidence.
