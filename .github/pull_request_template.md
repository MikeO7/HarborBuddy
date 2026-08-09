## Summary

<!-- What changed, and why is this the smallest robust solution? -->

## Safety impact

<!-- Describe Docker mutation, rollback, self-update, configuration, or security implications. Use N/A when not applicable. -->

- [ ] Dry-run still avoids recreating, renaming, stopping, or removing containers and images.
- [ ] Targets are re-inspected immediately before replacement.
- [ ] The original container remains recoverable until readiness succeeds.
- [ ] Failure restores the original name, networks, restart policy, and running state where Docker permits it.
- [ ] Unsupported Docker configurations are rejected rather than recreated unsafely.
- [ ] Self-update preserves explicit daemon identity and helper isolation.
- [ ] No Docker credentials, certificates, tokens, environment dumps, or sensitive logs are exposed.
- [ ] N/A — this change does not affect these invariants.

## Verification

<!-- List exact commands and results. Do not check a box for a skipped command. -->

- [ ] `make fmt-check source-limits`
- [ ] `make vet lint vuln`
- [ ] `make test-cover test-race build`
- [ ] `make lint-nongo`
- [ ] `make docker-build`
- [ ] `make test-integration`
- [ ] N/A — Docker checks are not relevant.

## Coverage and failure paths

- [ ] New behavior has focused success, cancellation, unsupported-input, and rollback/failure tests where practical.
- [ ] Package coverage floors were maintained or intentionally raised.
- [ ] Any generated output is reproducible, marked as generated, and committed with its source.

## Documentation

- [ ] Configuration, examples, README, security guidance, and migration notes match the implementation.
- [ ] `CHANGELOG.md` includes an Unreleased entry when behavior is user-visible.
- [ ] Known limitations and skipped verification are stated explicitly.
