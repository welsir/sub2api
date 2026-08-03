/**
 * [INPUT]: Compiled adapter modules and a local fake Qwen backend.
 * [OUTPUT]: Emitted-runtime gzip/server smoke and structured main-process listen-error proof.
 * [POS]: Integration coverage for the exact dist artifact used by the adapter image.
 *
 * [PROTOCOL]:
 * 1. Update this header when runtime smoke responsibilities change.
 * 2. Update this folder's .folder.md when this file changes.
 */

import { spawn } from "node:child_process";
import { once } from "node:events";
import { createServer } from "node:http";
import { fileURLToPath } from "node:url";
import { gzipSync } from "node:zlib";
import { describe, expect, it } from "vitest";

import { resolveAdapterConfig } from "../../dist/config.js";
import { createModerationAdapterServer } from "../../dist/server.js";

async function listen(server) {
  await new Promise((resolve) => server.listen(0, "127.0.0.1", resolve));
  const { port } = server.address();
  return `http://127.0.0.1:${port}`;
}

async function close(server) {
  server.closeAllConnections?.();
  await new Promise((resolve) => server.close(resolve));
}

describe("compiled moderation adapter runtime", () => {
  it("serves a gzip moderation contract through emitted modules", async () => {
    const backend = createServer((_request, response) => {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(
        JSON.stringify({
          choices: [{ message: { content: "Safety: Unsafe\nCategories: Jailbreak" }, finish_reason: "stop" }]
        })
      );
    });
    const backendBaseUrl = await listen(backend);
    const adapter = createModerationAdapterServer(
      resolveAdapterConfig({
        QWEN3GUARD_ADAPTER_BEARER_TOKEN: "runtime-token",
        QWEN3GUARD_BACKEND_BASE_URL: backendBaseUrl,
        QWEN3GUARD_BACKEND_MODEL: "qwen3guard-runtime"
      }),
      { log() {} }
    );
    const adapterBaseUrl = await listen(adapter);

    try {
      const response = await fetch(`${adapterBaseUrl}/v1/moderations`, {
        method: "POST",
        headers: {
          authorization: "Bearer runtime-token",
          "content-type": "application/json",
          "content-encoding": "gzip"
        },
        body: gzipSync(
          Buffer.from(JSON.stringify({ model: "moderation-local", input: "runtime fixture" }))
        )
      });
      const body = await response.json();

      expect(response.status).toBe(200);
      expect(body.results[0].flagged).toBe(true);
      expect(body.results[0].category_scores.illicit).toBe(1);
    } finally {
      await close(adapter);
      await close(backend);
    }
  });

  it("reports a structured listen error and exits nonzero when the port is occupied", async () => {
    const occupied = createServer();
    const occupiedBaseUrl = await listen(occupied);
    const { port } = new URL(occupiedBaseUrl);
    const mainPath = fileURLToPath(
      new URL("../../dist/main.js", import.meta.url)
    );
    const child = spawn(process.execPath, [mainPath], {
      env: {
        ...process.env,
        QWEN3GUARD_ADAPTER_HOST: "127.0.0.1",
        QWEN3GUARD_ADAPTER_PORT: port,
        QWEN3GUARD_ADAPTER_BEARER_TOKEN: "listen-error-test-token",
        QWEN3GUARD_BACKEND_BASE_URL: "http://127.0.0.1:1",
        QWEN3GUARD_BACKEND_MODEL: "qwen3guard-runtime"
      },
      stdio: ["ignore", "pipe", "pipe"]
    });
    let output = "";
    child.stdout.on("data", (chunk) => {
      output += chunk.toString();
    });
    child.stderr.on("data", (chunk) => {
      output += chunk.toString();
    });

    const [exitCode] = await once(child, "exit");
    await close(occupied);

    expect(exitCode).toBe(1);
    expect(output).toContain('"event":"qwen3guard_adapter.listen_error"');
    expect(output).not.toContain("listen-error-test-token");
  });
});
