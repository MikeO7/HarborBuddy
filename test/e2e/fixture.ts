import { spawn, type ChildProcess } from "node:child_process";
import { createServer } from "node:http";
import { mkdtemp, writeFile, rm, mkdir } from "node:fs/promises";
import { expect, test as base } from "e2e";

export const oldID = "a".repeat(64), newID = "b".repeat(64);
export const oldImage = `sha256:${"1".repeat(64)}`, newImage = `sha256:${"2".repeat(64)}`;
const oldDate = "2020-01-01T00:00:00Z";
export function container(id = oldID, name = "app", image = oldImage): any {
  return { Id: id, Name: `/${name}`, Image: image, Created: oldDate,
    Config: { Image: "example/app:latest", Env: ["SETTING=kept", "SECRET=synthetic-only"], Labels: {},
      Healthcheck: { Test: ["CMD", "true"] }, Cmd: ["serve"], User: "1000" },
    HostConfig: { RestartPolicy: { Name: "unless-stopped", MaximumRetryCount: 0 }, NetworkMode: "bridge",
      Binds: ["/fixture/data:/data:rw"] }, Mounts: [],
    NetworkSettings: { Networks: { bridge: { NetworkID: "network-bridge", Aliases: ["app"] } } },
    State: { Running: true, Status: "running", Pid: 101, StartedAt: oldDate, Health: { Status: "healthy" } } };
}
export type Result = { code: number | null; stdout: string; stderr: string; events: any[] };
export type Request = { method: string; path: string; query: URLSearchParams; body: any };
export type Fixture = {
  host: string; dir: string; requests: Request[]; unexpected: string[]; containers: Map<string, any>;
  images: any[]; networks: any[]; volumes: any[]; caches: any[]; targetImage: string; pullBody: string;
  failure: { method: string; path: string; status: number } | null;
  createdImage: string | null; unhealthy: boolean; changed: boolean; helperReady: boolean;
  run: (args?: string[], options?: { config?: string; env?: Record<string, string>; stopOn?: string; socket?: boolean }) => Promise<Result>;
  mutations: () => Request[];
};
const config = `updates:\n  self_update: false\n  stop_timeout: 1s\n  startup_timeout: 1s\ncleanup:\n  enabled: false\nlog:\n  level: debug\n  json: true\n`;

export const test = base.extend<{ docker: Fixture }>({
  docker: async (_fixtures, use) => {
    const dir = await mkdtemp(`${process.env.TMPDIR || "/tmp"}/hb-e2e-`);
    await mkdir(".e2e/fixtures", { recursive: true });
    const children = new Set<ChildProcess>();
    let invocation = 0;
    const f: Fixture = { dir, host: "tcp://127.0.0.1:39070", requests: [], unexpected: [],
      containers: new Map([[oldID, container()]]), images: [], networks: [], volumes: [], caches: [],
      targetImage: newImage, pullBody: '{"status":"Downloaded newer image"}\n', failure: null,
      createdImage: null, unhealthy: false, changed: false, helperReady: true,
      mutations: () => f.requests.filter(r => r.method !== "GET" && r.method !== "HEAD" && r.path !== "/images/create" && !r.path.endsWith("/wait")),
      run: async (args = ["--once"], options = {}) => {
        const path = `${dir}/config.yml`;
        await writeFile(path, options.config ?? config, { mode: 0o600 });
        const env: Record<string, string> = { PATH: process.env.PATH!, HOME: dir,
          HARBORBUDDY_DOCKER_HOST: options.socket ? `unix://${dir}/docker.sock` : f.host,
          HARBORBUDDY_LOG_JSON: "true", ...options.env };
        const child = spawn(`${process.cwd()}/.e2e/bin/harborbuddy`, ["--config", path, ...args], { env });
        children.add(child);
        let stdout = "", stderr = "", stopped = false;
        const timeout = setTimeout(() => child.kill("SIGKILL"), 10_000);
        child.stdout.on("data", data => {
          stdout += data;
          if (options.stopOn && stdout.includes(`"event":"${options.stopOn}"`) && !stopped) {
            stopped = true; child.kill("SIGTERM");
          }
        });
        child.stderr.on("data", data => { stderr += data; });
        const code = await new Promise<number | null>((resolve, reject) => { child.once("error", reject); child.once("close", resolve); });
        clearTimeout(timeout); children.delete(child);
        const events = stdout.split("\n").filter(line => line.startsWith("{")).map(line => JSON.parse(line));
        await writeFile(`${process.cwd()}/.e2e/fixtures/${dir.split("/").at(-1)}-${invocation++}.json`, JSON.stringify({ code, args: args.map(a => a.length > 256 ? `${a.slice(0, 256)} [truncated ${a.length} chars]` : a), config: options.config ?? config, stdout, stderr, requests: f.requests.map(r => ({ ...r, query: Object.fromEntries(r.query) })) }));
        return { code, stdout, stderr, events };
      },
    };
    const handler = async (req: any, res: any) => {
      const url = new URL(req.url, "http://fixture");
      const path = decodeURIComponent(url.pathname.replace(/^\/v[0-9.]+/, ""));
      const chunks: Buffer[] = []; for await (const chunk of req) chunks.push(chunk);
      const raw = Buffer.concat(chunks).toString();
      const r: Request = { method: req.method, path, query: url.searchParams, body: raw ? JSON.parse(raw) : null };
      f.requests.push(r);
      const reply = (value: any, status = 200) => { res.writeHead(status, { "Content-Type": "application/json" }); res.end(JSON.stringify(value)); };
      if ((f.failure?.method === r.method || (f.failure?.method === "HEAD" && r.method === "GET")) && f.failure?.path === path) return reply({ message: "fixture failure" }, f.failure.status);
      if (path === "/_ping") { res.writeHead(200, { "API-Version": "1.41" }); return res.end("OK"); }
      if (path === "/containers/json") return reply([...f.containers.values()].filter(c => r.query.get("all") === "1" || c.State.Running).map(c => ({
        Id: c.Id, Names: [c.Name], Image: c.Config.Image, ImageID: c.Image, Labels: c.Config.Labels,
        Created: Date.parse(c.Created) / 1000, State: c.State.Status, SizeRw: 100,
      })));
      if (path === "/images/create") { res.writeHead(200, { "Content-Type": "application/json" }); return res.end(f.pullBody); }
      if (path === "/images/json") return reply(r.query.get("filters")?.includes("dangling") ? f.images.filter(i => i.RepoTags.length === 0) : f.images);
      if (path.startsWith("/images/") && path.endsWith("/json")) return reply({ Id: f.targetImage, RepoTags: ["example/app:latest"], Created: oldDate, Config: {} });
      if (path === "/system/df") return reply({ Images: f.images, Volumes: f.volumes, BuildCache: f.caches, LayersSize: 0 });
      if (path === "/networks" && r.method === "GET") return reply(f.networks);
      if (path === "/build/prune") return reply({ CachesDeleted: f.caches.filter(c => !c.InUse).map(c => c.ID), SpaceReclaimed: 100 });
      if (path === "/containers/create") {
        const helper = r.body.HostConfig.AutoRemove;
        const id = helper ? "c".repeat(64) : newID;
        const c = container(id, r.query.get("name")!, f.createdImage ?? f.targetImage);
        c.Config = { ...r.body }; delete c.Config.HostConfig; delete c.Config.NetworkingConfig;
        c.HostConfig = r.body.HostConfig; c.NetworkSettings = { Networks: r.body.NetworkingConfig?.EndpointsConfig ?? {} };
        c.State.Running = false; c.State.Status = "created";
        if (f.unhealthy) c.State.Health.Status = "unhealthy";
        f.containers.set(id, c); return reply({ Id: id, Warnings: [] }, 201);
      }
      const match = path.match(/^\/containers\/([^/]+)(?:\/(.+))?$/);
      if (match) {
        const c = f.containers.get(match[1]!); if (!c) return reply({ message: "No such container" }, 404);
        switch (match[2]) {
          case "json": return reply(f.changed && c.Id === oldID ? { ...c, Image: `sha256:${"3".repeat(64)}` } : c);
          case "logs": res.writeHead(200, { "Content-Type": "application/vnd.docker.raw-stream" }); return res.end(f.helperReady ? "HARBORBUDDY_SELF_UPDATE_HELPER_READY\n" : "");
          case "wait": c.State.Running = false; c.State.Status = "exited"; return reply({ StatusCode: 0 });
          case "update": c.HostConfig.RestartPolicy = r.body.RestartPolicy; return reply({ Warnings: [] });
          case "stop": c.State.Running = false; c.State.Status = "exited"; return reply(null, 204);
          case "start": c.State.Running = true; c.State.Status = "running"; return reply(null, 204);
          case "rename": c.Name = `/${r.query.get("name")}`; return reply(null, 204);
          default: if (r.method === "DELETE") { f.containers.delete(c.Id); return reply(null, 204); }
        }
      }
      if (path.startsWith("/networks/")) {
        if (r.method === "POST" || r.method === "DELETE") return reply(null, 204);
        return reply({ ...f.networks.find(n => n.Id === path.split("/")[2]), Containers: {}, Services: {} });
      }
      if (r.method === "DELETE" && (path.startsWith("/images/") || path.startsWith("/volumes/"))) return reply([]);
      f.unexpected.push(`${r.method} ${path}`); return reply({ message: `Unhandled fixture route ${r.method} ${path}` }, 404);
    };
    const tcp = createServer(handler), socket = createServer(handler);
    await new Promise<void>(resolve => tcp.listen(39070, "127.0.0.1", resolve));
    await new Promise<void>(resolve => socket.listen(`${dir}/docker.sock`, resolve));
    try { await use(f); expect(f.unexpected).toEqual([]); } finally {
      for (const child of children) child.kill("SIGKILL");
      await Promise.all([tcp, socket].map(server => new Promise<void>((resolve, reject) => server.close(err => err ? reject(err) : resolve()))));
      await rm(dir, { recursive: true, force: true });
    }
  },
});
