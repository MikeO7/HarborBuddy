import { expect } from "e2e";
import { test } from "./fixture";

for (const [name, args, config, env] of [
  ["unknown flag", ["--unknown"], undefined, {}],
  ["missing flag value", ["--interval"], undefined, {}],
  ["malformed duration", ["--interval=no"], undefined, {}],
  ["negative interval", ["--interval=-1s"], undefined, {}],
  ["zero interval", ["--interval=0"], undefined, {}],
  ["invalid schedule", ["--schedule-time=24:00"], undefined, {}],
  ["unknown timezone", ["--schedule-time=12:00", "--timezone=unknown/place"], undefined, {}],
  ["unknown log level", ["--log-level=trace"], undefined, {}],
  ["missing explicit config", ["--config=/missing/harborbuddy.yml"], undefined, {}],
  ["unknown YAML key", [], "updates:\n  unknown: true\n", {}],
  ["duplicate YAML key", [], "updates:\n  enabled: true\n  enabled: false\n", {}],
  ["multiple documents", [], "updates: {}\n---\nupdates: {}\n", {}],
  ["malformed YAML", [], "updates: [\n", {}],
  ["middle wildcard", [], 'updates:\n  allow_images: ["ap*p"]\n', {}],
  ["multiple wildcards", [], 'updates:\n  deny_images: ["**"]\n', {}],
  ["negative retention", [], "updates:\n  rollback_image_retention: -1\n", {}],
  ["zero stop timeout", [], "updates:\n  stop_timeout: 0s\n", {}],
  ["negative startup", [], "updates:\n  startup_timeout: -1s\n", {}],
  ["negative cleanup age", [], "cleanup:\n  min_age_hours: -1\n", {}],
  ["overflow cleanup age", [], "cleanup:\n  min_age_hours: 9223372036854775807\n", {}],
  ["invalid boolean env", [], undefined, { HARBORBUDDY_DRY_RUN: "sometimes" }],
  ["invalid integer env", [], undefined, { HARBORBUDDY_LOG_MAX_SIZE: "NaN" }],
  ["zero log size", [], undefined, { HARBORBUDDY_LOG_MAX_SIZE: "0" }],
  ["negative log backups", [], undefined, { HARBORBUDDY_LOG_MAX_BACKUPS: "-1" }],
  ["missing helper identity", ["--updater-mode"], undefined, {}],
  ["negative helper timeout", ["--updater-mode", "--target-container-id=x", "--new-image-id=y", "--helper-stop-timeout=-1s"], undefined, {}],
  ["negative helper retries", ["--updater-mode", "--target-container-id=x", "--new-image-id=y", "--helper-restart-max-retries=-1"], undefined, {}],
  ["extra after separator", ["--once", "--", "secret"], undefined, {}],
  ["oversized extra", ["--version", "x".repeat(65536)], undefined, {}],
] as const) {
  test(`reject ${name} before Docker or logger file side effects`, async ({ docker }) => {
    const r = await docker.run([...args], { config, env: { ...env, HARBORBUDDY_LOG_FILE: `${docker.dir}/unexpected.log` } });
    expect(r.code).toBe(1); expect(docker.requests).toHaveLength(0);
    const { access } = await import("node:fs/promises");
    expect(await access(`${docker.dir}/unexpected.log`).then(() => true, () => false)).toBe(false);
  });
}

test("CLI overrides environment which overrides YAML", async ({ docker }) => {
  const r = await docker.run(["--once", "--dry-run", "--log-level=debug"], {
    config: "updates:\n  dry_run: false\n  self_update: false\ncleanup:\n  enabled: false\nlog:\n  level: error\n  json: true\n",
    env: { HARBORBUDDY_DRY_RUN: "false", HARBORBUDDY_LOG_LEVEL: "warn" },
  });
  expect(r.events.find(e => e.event === "container_update_result")?.result).toBe("would_update");
  expect(docker.mutations()).toHaveLength(0);
});
