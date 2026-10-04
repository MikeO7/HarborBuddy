import type { E2EConfig } from "e2e";
export default {
  tests: "test/e2e/**/*.e2e.ts",
  targets: [{ name: "docker-protocol", platform: "cli" }],
  workers: 1,
  retries: 0,
  reporters: ["list", "junit", "markdown"],
} satisfies E2EConfig;
