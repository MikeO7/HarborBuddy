# Observable flow inventory

E2E executes the production binary against an isolated Docker protocol fixture.
`make test-integration` remains the primary proof for actual Docker/Podman
semantics and autonomous helper execution.

| Public flow | Primary coverage | Success, rejection, and error boundary |
| --- | --- | --- |
| CLI/version/help | `flows.e2e.ts`, `config.e2e.ts`, `regressions.e2e.ts`; `internal/app/*test.go` | Version/help avoid Docker; unknown/missing flags, durations, extra/separator/oversized arguments fail before Docker/log creation. |
| YAML/env/flags | `config.e2e.ts`; `internal/config/*test.go`, `internal/app/*test.go` | Precedence; unknown/duplicate/multiple/malformed YAML; invalid booleans/integers/patterns/schedules/timezones/limits. Owner tests cover required versus optional config, migration, and defaults. |
| Docker connection | `flows.e2e.ts`; `internal/docker/adapter_test.go`, `client_logic_test.go` | HTTP/Unix socket; ping/list/inspect/pull errors. Owner tests cover remote host/TLS options; live TLS handshake remains environment proof. |
| Discovery/identity/filter | `flows.e2e.ts`, `selfupdate.e2e.ts`; `internal/updater/*test.go`, `internal/selfupdate/*test.go` | Exact/prefix/suffix allows, deny precedence, opt-out/self opt-out, shared pull cache, external identity change. Owner tests cover daemon/helper exclusion and ambiguous identity. |
| Pull/current/dry-run | `flows.e2e.ts`, `regressions.e2e.ts`; `pull_response_test.go`, `adapter_test.go` | Current no-op; dry-run pulls without replacement/deletion; HTTP/streamed/legacy/late errors, malformed/wrong/null/missing/empty/oversized responses fail before cache inspection/replacement. Owner tests cover read/close errors. |
| Replacement | `flows.e2e.ts`; `internal/docker/replace*test.go`, `replacement_config*test.go`; live integration | Preserve env, mounts, name, restart; readiness precedes original removal. Reject auto-remove, shared namespaces, Swarm, unsafe mounts. Owner tests cover timeouts, endpoint/config copies, and each transport stage. |
| Rollback/retention | `flows.e2e.ts`, `selfupdate.e2e.ts`; `rollback_images_test.go`, `replace_branches_test.go`; live integration | Create/start/image mismatch/unhealthy failures restore original running name/restart and remove replacement. Owner tests cover failed rollback, stop/rename/network errors, backup warnings, slot rotation/in-use protection. |
| Cleanup | `cleanup.e2e.ts`; `internal/cleanup/*test.go`, `internal/docker/cleanup_test.go` | Dangling-only/all-resource success/dry-run; age, referenced image, rollback-tag, running container, default network, used/unknown-volume protection; build prune; list failure no deletion. Owner tests cover remove/inspect/prune errors and opt-in categories. |
| Lifecycle/scheduler/logs | `flows.e2e.ts`; `internal/scheduler/*test.go`, `internal/logging/*test.go`, `cmd/harborbuddy/*test.go` | Immediate interval cycle, daily next-run calculation, SIGTERM. Owner tests cover DST/calendar edges, SIGUSR1, repeated cancellation, writer/rotation/close errors. Long-running rotation remains environment proof. |
| Self-update handoff | `selfupdate.e2e.ts`; `internal/docker/selfupdate*test.go`, `internal/updater/selfupdate_flow_test.go`; live integration | Restricted helper env/mounts, readiness acknowledgement, restart suppression, successful exit; dry-run/disable; unready helper cleanup preserves original. Owner tests cover duplicate helpers, labels, transport/config errors. |
| Helper mode | `selfupdate.e2e.ts`, `config.e2e.ts`; `internal/selfupdate/updater_test.go`, `internal/app/*test.go`; live integration | Required identity/timeouts/retries fail before Docker; wait API then healthy replacement or unhealthy rollback. Fixture runs daemon/helper stages separately. |
| Build/publication | Existing CI `Container smoke test`, `Validate image`, multi-platform workflows, live integration | Actual scratch image, architectures, registry, Docker/Podman health and autonomous helper behavior are hosted/live-engine gates. |

No provider key or model is used. The fixture owns Docker responses and mutations
in memory. Child environments exclude inherited Docker endpoints, TLS secrets,
and user config. Retries are zero. Teardown closes listeners, kills remaining
children, and removes private config. Unexpected routes fail verification.

Evidence includes revision, command, environment, report, and per-invocation
config, output, and protocol requests. Synthetic values are recorded explicitly.
