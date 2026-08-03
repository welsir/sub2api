/**
 * [INPUT]: Adapter package metadata, Docker build definition, and emitted runtime modules.
 * [OUTPUT]: Proof that one TypeScript source builds the Docker runtime with current MiniMax behavior.
 * [POS]: Release-path regression coverage preventing checked-in JavaScript from drifting from tested code.
 *
 * [PROTOCOL]:
 * 1. Update this header when emitted-runtime responsibilities change.
 * 2. Update this folder's .folder.md when this file changes.
 */

import { readdir, readFile } from "node:fs/promises";
import { fileURLToPath, pathToFileURL } from "node:url";

import { describe, expect, it } from "vitest";

const adapterRoot = fileURLToPath(new URL("../..", import.meta.url));
const sourceRoot = fileURLToPath(
  new URL("../../src/qwen3guard-moderation-adapter/", import.meta.url)
);

async function packageMetadata(): Promise<Record<string, unknown>> {
  return JSON.parse(
    await readFile(fileURLToPath(new URL("../../package.json", import.meta.url)), "utf8")
  ) as Record<string, unknown>;
}

async function emittedModule(name: string): Promise<Record<string, any>> {
  const packageJson = await packageMetadata();
  const scripts = packageJson.scripts as Record<string, string> | undefined;
  expect(scripts?.build).toBe("tsc -p tsconfig.build.json");
  const moduleUrl = pathToFileURL(`${adapterRoot}/dist/${name}.js`);
  return await import(`${moduleUrl.href}?test=${Date.now()}`) as Record<string, any>;
}

describe("compiled moderation adapter runtime", () => {
  it("uses emitted TypeScript as the only JavaScript runtime source", async () => {
    const packageJson = await packageMetadata();
    const scripts = packageJson.scripts as Record<string, string> | undefined;
    const dockerfile = await readFile(
      fileURLToPath(new URL("../../Dockerfile", import.meta.url)),
      "utf8"
    );
    const checkedInJavaScript = (await readdir(sourceRoot))
      .filter((name) => name.endsWith(".js"))
      .sort();

    expect(scripts?.build).toBe("tsc -p tsconfig.build.json");
    expect(scripts?.start).toContain("dist/main.js");
    expect(checkedInJavaScript).toEqual([]);
    expect(dockerfile).toContain("RUN pnpm run build");
    expect(dockerfile).toMatch(/COPY --from=build \/app\/dist \.\/dist/);
    expect(dockerfile).toContain('CMD ["node", "dist/main.js"]');
  });

  it("emits the high-speed MiniMax request without service_tier", async () => {
    const { buildMiniMaxChatRequest } = await emittedModule("minimax");
    const request = buildMiniMaxChatRequest(
      "MiniMax-M2.7-highspeed",
      "runtime transcript",
      "priority"
    );

    expect(request.model).toBe("MiniMax-M2.7-highspeed");
    expect(Object.hasOwn(request, "service_tier")).toBe(false);
    expect(request.reasoning_split).toBe(true);
  });

  it("emits deterministic billing classification for MiniMax code 2056", async () => {
    const { createBackendClient } = await emittedModule("backend");
    const { resolveAdapterConfig } = await emittedModule("config");
    const client = createBackendClient(
      resolveAdapterConfig({
        QWEN3GUARD_ADAPTER_BEARER_TOKEN: "adapter-token",
        QWEN3GUARD_BACKEND_PROVIDER: "minimax",
        QWEN3GUARD_BACKEND_BASE_URL: "https://api.minimaxi.com",
        QWEN3GUARD_BACKEND_MODEL: "MiniMax-M2.7-highspeed",
        QWEN3GUARD_BACKEND_BEARER_TOKEN: "minimax-token"
      }),
      async () => new Response(JSON.stringify({
        error: {
          type: "rate_limit_error",
          code: null,
          message: "Token Plan usage limit exceeded (2056)"
        }
      }), {
        status: 429,
        headers: { "content-type": "application/json" }
      })
    );

    await expect(
      client.classify("runtime transcript", new AbortController().signal)
    ).rejects.toMatchObject({ kind: "billing", providerCode: 2056 });
  });
});
