import { expect } from "e2e";
import { test } from "./fixture";

test("positional arguments are rejected before Docker contact", async ({ docker }) => {
  const result = await docker.run(["--once", "unexpected"]);
  expect(result.code).toBe(1);
  expect(result.stderr).toContain("positional arguments");
  expect(docker.requests).toHaveLength(0);
});

test("HTTP200 pull stream error fails without inspecting cache or replacing containers", async ({ docker }) => {
  docker.pullBody = '{"errorDetail":{"message":"fixture registry denied"},"error":"fixture registry denied"}\n';
  const result = await docker.run();
  expect(result.events.find(e => e.event === "container_update_result")?.result).toBe("failed");
  expect(result.events.find(e => e.event === "container_update_result")?.error).toContain("fixture registry denied");
  expect(docker.requests.filter(r => r.path.startsWith("/images/") && r.path.endsWith("/json"))).toHaveLength(0);
  expect(docker.mutations()).toHaveLength(0);
});

for (const [name, body] of [
  ["legacy error", '{"error":"fixture denied"}'],
  ["late error", '{"status":"Pulling"}\n{"errorDetail":{"message":"fixture denied"}}'],
  ["malformed", '{"status":'], ["wrong type", '[]'], ["null", 'null'],
  ["missing fields", '{}'], ["empty", ' \n'], ["oversized", ' '.repeat(16 * 1024 * 1024 + 1)],
] as const) {
  test(`pull stream rejects ${name} without cached image inspection or replacement`, async ({ docker }) => {
    docker.pullBody = body;
    const r = await docker.run();
    expect(r.events.find(e => e.event === "container_update_result")?.result).toBe("failed");
    expect(docker.requests.filter(r => r.path.startsWith("/images/") && r.path.endsWith("/json"))).toHaveLength(0);
    expect(docker.mutations()).toHaveLength(0);
  });
}
