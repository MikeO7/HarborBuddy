import { execFileSync, spawnSync } from "node:child_process";
import { mkdirSync, writeFileSync } from "node:fs";
mkdirSync(".e2e/bin", { recursive: true });
const revision = execFileSync("git", ["rev-parse", "HEAD"], { encoding: "utf8" }).trim();
const buildCommand = ["build", "BINARY_NAME=.e2e/bin/harborbuddy"];
execFileSync("make", buildCommand, { stdio: "inherit" });
const cli = new URL("cli/bin.js", import.meta.resolve("e2e"));
const args = [cli.pathname, "run", ...process.argv.slice(2)];
writeFileSync(".e2e/environment.json", JSON.stringify({
  revision, dirty: execFileSync("git", ["status", "--porcelain"], { encoding: "utf8" }).trim() !== "",
  command: [process.execPath, ...args], buildCommand: ["make", ...buildCommand], node: process.version,
  go: execFileSync("go", ["version"], { encoding: "utf8" }).trim(), platform: process.platform,
  startedAt: new Date().toISOString(),
  testData: "Disposable per-attempt config, containers, images, networks, volumes and cache in memory.",
  environment: "Production binary against isolated Docker HTTP39070/Unix-socket protocol fixture. No live Docker/Podman, image pull, registry, or container mutation.",
}, null, 2) + "\n");
const result = spawnSync(process.execPath, args, { stdio: "inherit", env: { ...process.env, E2E_TELEMETRY_DISABLED: "1" } });
process.exitCode = result.status ?? 3;
