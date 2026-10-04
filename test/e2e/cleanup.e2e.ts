import { expect } from "e2e";
import { oldID, test } from "./fixture";
const ancient = 1577836800, created = "2020-01-01T00:00:00Z";
function seed(docker: any) {
  docker.images = [
    { Id: "sha256:old-dangling", RepoTags: [], Created: ancient, Size: 100, Containers: 0 },
    { Id: "sha256:young", RepoTags: [], Created: Math.floor(Date.now()/1000), Size: 100, Containers: 0 },
    { Id: "sha256:tagged", RepoTags: ["example/old:tag"], Created: ancient, Size: 100, Containers: 0 },
    { Id: "sha256:used", RepoTags: ["example/used:tag"], Created: ancient, Size: 100, Containers: 1 },
    { Id: "sha256:rollback", RepoTags: ["localhost/harborbuddy-rollback/app:old"], Created: ancient, Size: 100, Containers: 0 },
  ];
  docker.networks = [{ Id: "net-free", Name: "isolated", Scope: "local", Created: created }, { Id: "net-default", Name: "bridge", Scope: "local", Created: created }];
  docker.volumes = [{ Name: "vol-free", CreatedAt: created, UsageData: { RefCount: 0, Size: 100 } }, { Name: "vol-used", CreatedAt: created, UsageData: { RefCount: 1, Size: 100 } }, { Name: "vol-unknown", CreatedAt: created }];
  docker.caches = [{ ID: "cache-free", CreatedAt: created, LastUsedAt: created, Size: 100, InUse: false }, { ID: "cache-used", CreatedAt: created, LastUsedAt: created, Size: 100, InUse: true }];
  const c = structuredClone(docker.containers.get(oldID)); c.Id = "stopped"; c.Name = "/stopped"; c.State.Running = false; c.State.Status = "exited";
  docker.containers.set(c.Id, c);
}
const config = (all: boolean) => `updates:\n  enabled: false\ncleanup:\n  enabled: true\n  all: ${all}\n  min_age_hours: 24\nlog:\n  json: true\n  level: debug\n`;

for (const all of [false, true]) {
  for (const dry of [false, true]) {
    test(`cleanup ${all ? "all resources" : "dangling only"} ${dry ? "dry-run" : "removes eligible only"}`, async ({ docker }) => {
      seed(docker);
      const r = await docker.run(["--cleanup-only", ...(dry ? ["--dry-run"] : [])], { config: config(all) });
      expect(r.code).toBe(0); expect(docker.unexpected).toEqual([]);
      const removed = docker.mutations().map(r => `${r.method} ${r.path}`);
      if (dry) expect(removed).toEqual([]);
      else {
        expect(removed).toContain("DELETE /images/sha256:old-dangling");
        expect(removed).not.toContain("DELETE /images/sha256:young");
        if (all) {
          expect(removed).toContain("DELETE /images/sha256:tagged");
          expect(removed).not.toContain("DELETE /images/sha256:used");
          expect(removed).not.toContain("DELETE /images/sha256:rollback");
          expect(removed).toContain("DELETE /containers/stopped");
          expect(removed).toContain("DELETE /networks/net-free");
          expect(removed).not.toContain("DELETE /networks/net-default");
          expect(removed).toContain("DELETE /volumes/vol-free");
          expect(removed).not.toContain("DELETE /volumes/vol-used");
          expect(removed).not.toContain("DELETE /volumes/vol-unknown");
          expect(removed).toContain("POST /build/prune");
        } else expect(removed).toEqual(["DELETE /images/sha256:old-dangling"]);
      }
      expect(docker.containers.get(oldID).State.Running).toBe(true);
      expect(docker.requests.filter(r => r.path === "/images/create")).toHaveLength(0);
    });
  }
}

test("cleanup list failure reports failure without deleting", async ({ docker }) => {
  docker.failure = { method: "GET", path: "/images/json", status: 500 };
  const r = await docker.run(["--cleanup-only"], { config: config(false) });
  expect(r.code).toBe(1); expect(docker.mutations()).toHaveLength(0);
});
