import { expect } from "e2e";
import { newID, newImage, oldID, test } from "./fixture";
const helperID = "c".repeat(64);
const selfConfig = "updates:\n  self_update: true\ncleanup:\n  enabled: false\nlog:\n  json: true\n  level: debug\n";
const selfEnv = { HARBORBUDDY_CONTAINER_ID: oldID };

test("self-update launches ready restricted helper then hands off successfully", async ({ docker }) => {
  const current = docker.containers.get(oldID);
  current.Config.Env.push("DOCKER_HOST=tcp://127.0.0.1:39070");
  const r = await docker.run(["--once"], { config: selfConfig, env: selfEnv });
  expect(r.code).toBe(0);
  expect(r.events.find(e => e.event === "container_update_result")?.result).toBe("self_update_started");
  expect(r.events.some(e => e.event === "daemon_self_update_handoff")).toBe(true);
  const helper = docker.containers.get(helperID);
  expect(helper.HostConfig.AutoRemove).toBe(true); expect(helper.HostConfig.RestartPolicy.Name).toBe("no");
  expect(helper.Config.Image).toBe(newImage);
  expect(helper.Config.Env.some((s: string) => s.startsWith("SECRET=") || s.startsWith("SETTING="))).toBe(false);
  expect(helper.HostConfig.Binds ?? []).not.toContain("/fixture/data:/data:rw");
  expect(helper.Config.Cmd).toContain("--updater-mode");
  expect(current.HostConfig.RestartPolicy.Name).toBe("no");
  expect(current.State.Running).toBe(true);
  expect(docker.unexpected).toEqual([]);
});

test("self-update dry-run and opt-out preserve daemon and never launch helper", async ({ docker }) => {
  const r = await docker.run(["--once", "--dry-run"], { config: selfConfig, env: selfEnv });
  expect(r.events.find(e => e.event === "container_update_result")?.result).toBe("would_update");
  expect(docker.mutations()).toHaveLength(0);
  docker.requests.length = 0;
  const disabled = await docker.run(["--once"], { config: selfConfig, env: { ...selfEnv, HARBORBUDDY_SELF_UPDATE_ENABLED: "false" } });
  expect(disabled.events.find(e => e.event === "container_update_result")?.result).toBe("excluded");
  expect(docker.requests.filter(r => r.path === "/images/create")).toHaveLength(0);
  expect(docker.mutations()).toHaveLength(0);
});

test("unready helper is removed without suppressing original restart", async ({ docker }) => {
  docker.helperReady = false;
  const r = await docker.run(["--once"], { config: selfConfig, env: selfEnv });
  expect(r.events.find(e => e.event === "container_update_result")?.result).toBe("failed");
  expect(docker.containers.has(helperID)).toBe(false);
  expect(docker.containers.get(oldID).HostConfig.RestartPolicy.Name).toBe("unless-stopped");
  expect(docker.containers.get(oldID).State.Running).toBe(true);
});

for (const unhealthy of [false, true]) {
  test(`helper mode waits for exit and ${unhealthy ? "rolls back failed replacement" : "completes replacement"}`, async ({ docker }) => {
    docker.unhealthy = unhealthy;
    const r = await docker.run(["--updater-mode", `--target-container-id=${oldID}`, `--new-image-id=${newImage}`,
      "--helper-startup-timeout=1s", "--helper-restart-policy=unless-stopped"], { env: { HARBORBUDDY_LOG_LEVEL: "debug" } });
    expect(r.stdout).toContain("HARBORBUDDY_SELF_UPDATE_HELPER_READY");
    expect(r.code).toBe(unhealthy ? 1 : 0);
    expect(docker.requests.filter(r => r.path === `/containers/${oldID}/wait`)).toHaveLength(1);
    expect(docker.containers.get(unhealthy ? oldID : newID).State.Running).toBe(true);
    expect(docker.containers.get(unhealthy ? oldID : newID).Name).toBe("/app");
    expect(r.events.some(e => e.event === (unhealthy ? "self_update_helper_failed" : "self_update_helper_complete"))).toBe(true);
    expect(docker.unexpected).toEqual([]);
  });
}
