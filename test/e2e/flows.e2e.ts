import { expect } from "e2e";
import { container, newID, oldID, oldImage, test } from "./fixture";
const resultEvent = (r: any) => r.events.find((e: any) => e.event === "container_update_result");
const noWrites = (docker: any) => { expect(docker.mutations()).toHaveLength(0); expect(docker.unexpected).toEqual([]); };

test("version and help never contact Docker", async ({ docker }) => {
  const version = await docker.run(["--version"]);
  expect(version.code).toBe(0); expect(version.stdout).toContain("HarborBuddy");
  const help = await docker.run(["--help"]); expect(help.stderr).toContain("--dry-run");
  expect(docker.requests).toHaveLength(0);
});

test("Unix socket transport dry-run pulls once and preserves original", async ({ docker }) => {
  const r = await docker.run(["--once", "--dry-run"], { socket: true });
  expect(r.code).toBe(0); expect(resultEvent(r).result).toBe("would_update");
  expect(docker.requests.filter(r => r.path === "/images/create")).toHaveLength(1);
  expect(docker.containers.get(oldID).State.Running).toBe(true); noWrites(docker);
});

test("current images stay running without replacement", async ({ docker }) => {
  docker.targetImage = oldImage;
  const r = await docker.run(); expect(resultEvent(r).result).toBe("current"); noWrites(docker);
});

test("successful replacement preserves configuration and releases original after ready", async ({ docker }) => {
  const r = await docker.run(); expect(r.code).toBe(0); expect(resultEvent(r).result).toBe("updated");
  const replacement = docker.containers.get(newID);
  expect(replacement.Name).toBe("/app"); expect(replacement.State.Running).toBe(true);
  expect(replacement.Config.Env).toEqual(["SETTING=kept", "SECRET=synthetic-only"]);
  expect(replacement.HostConfig.Binds).toEqual(["/fixture/data:/data:rw"]);
  expect(replacement.HostConfig.RestartPolicy.Name).toBe("unless-stopped");
  expect(docker.containers.has(oldID)).toBe(false);
  const start = docker.requests.findIndex(r => r.path === `/containers/${newID}/start`);
  const remove = docker.requests.findIndex(r => r.path === `/containers/${oldID}` && r.method === "DELETE");
  expect(remove).toBeGreaterThan(start); expect(docker.unexpected).toEqual([]);
});

for (const failure of ["unhealthy", "create", "start", "image-mismatch"] as const) {
  test(`replacement ${failure} restores original running name and restart policy`, async ({ docker }) => {
    if (failure === "unhealthy") docker.unhealthy = true;
    if (failure === "create") docker.failure = { method: "POST", path: "/containers/create", status: 500 };
    if (failure === "start") docker.failure = { method: "POST", path: `/containers/${newID}/start`, status: 500 };
    if (failure === "image-mismatch") {
      // A create response can identify the wrong image even after a successful pull.
      docker.createdImage = oldImage;
    }
    const r = await docker.run(); expect(resultEvent(r).result).toBe("failed");
    const original = docker.containers.get(oldID);
    expect(original.Name).toBe("/app"); expect(original.State.Running).toBe(true);
    expect(original.HostConfig.RestartPolicy.Name).toBe("unless-stopped");
    expect(docker.containers.has(newID)).toBe(false);
    expect(resultEvent(r).rollback_outcome).toBe("succeeded");
    expect(docker.unexpected).toEqual([]);
  });
}

for (const kind of ["auto-remove", "network namespace", "pid namespace", "ipc namespace", "uts namespace", "swarm", "unnamed volume", "unknown mount"] as const) {
  test(`unsupported ${kind} is rejected before replacement writes`, async ({ docker }) => {
    const c = docker.containers.get(oldID);
    if (kind === "auto-remove") c.HostConfig.AutoRemove = true;
    if (kind === "network namespace") c.HostConfig.NetworkMode = "container:other";
    if (kind === "pid namespace") c.HostConfig.PidMode = "container:other";
    if (kind === "ipc namespace") c.HostConfig.IpcMode = "container:other";
    if (kind === "uts namespace") c.HostConfig.UTSMode = "container:other";
    if (kind === "swarm") c.Config.Labels["com.docker.swarm.task.id"] = "task";
    if (kind === "unnamed volume") c.Mounts = [{ Type: "volume", Destination: "/extra" }];
    if (kind === "unknown mount") c.Mounts = [{ Type: "unknown", Destination: "/extra" }];
    const r = await docker.run(); expect(resultEvent(r).result).toBe("unsupported"); noWrites(docker);
  });
}

test("external image change skips transaction", async ({ docker }) => {
  docker.changed = true; const r = await docker.run(); expect(resultEvent(r).result).toBe("changed_externally"); noWrites(docker);
});

test("duplicate image references share a pull", async ({ docker }) => {
  docker.containers.set("d".repeat(64), container("d".repeat(64), "other"));
  const r = await docker.run(["--once", "--dry-run"]);
  expect(r.events.filter(e => e.event === "container_update_result")).toHaveLength(2);
  expect(docker.requests.filter(r => r.path === "/images/create")).toHaveLength(1); noWrites(docker);
});

for (const [name, env, label] of [
  ["exact allow", { HARBORBUDDY_ALLOW_IMAGES: "example/app:latest" }, null],
  ["prefix allow", { HARBORBUDDY_ALLOW_IMAGES: "example/*" }, null],
  ["suffix allow", { HARBORBUDDY_ALLOW_IMAGES: "*:latest" }, null],
  ["deny wins", { HARBORBUDDY_ALLOW_IMAGES: "*", HARBORBUDDY_DENY_IMAGES: "example/*" }, null],
  ["allow miss", { HARBORBUDDY_ALLOW_IMAGES: "other:*" }, null],
  ["optout", {}, "false"],
] as const) {
  test(`image filter ${name}`, async ({ docker }) => {
    if (label) docker.containers.get(oldID).Config.Labels["com.harborbuddy.autoupdate"] = label;
    const r = await docker.run(["--once", "--dry-run"], { config: `updates:\n  self_update: false\n  allow_images: ["${env.HARBORBUDDY_ALLOW_IMAGES ?? "*"}"]\n  deny_images: ["${env.HARBORBUDDY_DENY_IMAGES ?? "never"}"]\ncleanup:\n  enabled: false\nlog:\n  level: debug\n  json: true\n` });
    const allowed = name.includes("allow") && name !== "allow miss";
    expect(resultEvent(r).result).toBe(allowed ? "would_update" : "excluded");
    expect(docker.requests.filter(r => r.path === "/images/create")).toHaveLength(allowed ? 1 : 0); noWrites(docker);
  });
}

for (const [method, path] of [["HEAD", "/_ping"], ["GET", "/containers/json"], ["POST", "/images/create"], ["GET", "/images/example/app:latest/json"]]) {
  test(`Docker error ${method} ${path} preserves containers`, async ({ docker }) => {
    docker.failure = { method, path, status: 500 };
    const r = await docker.run();
    expect(r.code === 1 || resultEvent(r)?.result === "failed").toBe(true);
    expect(docker.containers.get(oldID).State.Running).toBe(true); noWrites(docker);
  });
}

test("interval daemon performs one immediate cycle and stops on SIGTERM", async ({ docker }) => {
  const r = await docker.run(["--dry-run", "--interval=1h"], { stopOn: "cycle_complete" });
  expect(r.code).toBe(0); expect(r.events.some(e => e.event === "daemon_stopped")).toBe(true); noWrites(docker);
});

test("daily scheduling computes next run without updating before SIGTERM", async ({ docker }) => {
  const r = await docker.run(["--schedule-time=23:59", "--timezone=UTC"], { stopOn: "scheduler_next_run" });
  expect(r.code).toBe(0); expect(docker.requests.filter(r => r.path === "/images/create")).toHaveLength(0); noWrites(docker);
});
