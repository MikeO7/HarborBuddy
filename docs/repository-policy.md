# Repository policy

## Required checks

Changes to `main` are expected to pass these stable GitHub Actions job names:

- `Go verification`
- `Code quality`
- `Secret scanning`
- `Container smoke test`
- `Validate image`

Release publication additionally requires `Publish multi-platform image` before `Publish GitHub release` runs. The scheduled `Multi-Platform Runtime Verification` workflow is an additional architecture smoke test rather than a pull-request gate.

The repository currently has no branch protection or repository rulesets. These checks are therefore documented and automated but do not block direct pushes or enforce approvals server-side. Configure a `main` branch rule to require pull requests, the checks above, resolved conversations, blocked force-pushes/deletion, and code-owner review where a second trusted reviewer is available.

Repository policy restricts Actions to GitHub-owned actions and the exact third-party action families used by the workflows, with full-length commit SHA pins enforced server-side. The in-repository `test/check-action-pins.sh` check remains a defense-in-depth guard for both `.yml` and `.yaml` workflow files.

All direct workflow actions must be pinned to full commit SHAs. `test/check-action-pins.sh` enforces that repository rule; Dependabot maintains the pins.

## Generated code

HarborBuddy currently commits no generated Go source. Any generated source introduced later must:

1. Use the standard `// Code generated ... DO NOT EDIT.` header.
2. Document the generator command and exact generator version.
3. Commit generator inputs and outputs together.
4. Be reproducible so CI can rerun generation and reject a dirty diff.
5. Never be edited by hand.

Generated files are exempt from selected size/complexity checks only when the standard header is present. Dependency changes must remain intentional and reproducible through `make tidy`.

## Secrets and scanning

Never include registry credentials, Docker authorization files, client certificates, private keys, access tokens, full environment dumps, or credential-bearing logs in commits, issues, pull requests, CI summaries, or artifacts.

Gitleaks scans repository history in CI. A passing scan reduces risk but does not prove that no secret exists. If a credential is disclosed, rotate or revoke it immediately before attempting history cleanup; removing it from Git history does not make an unrotated credential safe. Report disclosures privately through the security-advisory channel documented in `SECURITY.md`.

`govulncheck` blocks newly reachable Go vulnerabilities. The reviewed exceptions in `test/run-govulncheck.sh` are limited to unfixed Moby daemon and `docker cp` advisories whose affected server/copy APIs HarborBuddy does not run or invoke. Any new advisory fails the check.

Trivy blocks fixed HIGH and CRITICAL vulnerabilities in the built image and retains a SARIF artifact. GitHub's SARIF/code-scanning presentation depends on repository entitlement and settings; artifact generation and the blocking scan do not depend on that hosted feature.
