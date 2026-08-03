/**
 * [INPUT]: HTTP requests plus a local fake Qwen Chat Completions backend.
 * [OUTPUT]: HTTP, classifier-revision handshake, image fail-closed, stream-cancel, redaction, and lifecycle assertions.
 * [POS]: TypeScript contract suite for the standalone moderation adapter process.
 *
 * [PROTOCOL]:
 * 1. Update this header when HTTP contract responsibilities change.
 * 2. Update this folder's .folder.md when this file changes.
 */

import { createServer, type Server } from "node:http";
import { connect, type AddressInfo, type Socket } from "node:net";
import { gzipSync } from "node:zlib";
import { afterEach, describe, expect, it } from "vitest";

import {
  MAX_BACKEND_RESPONSE_BYTES,
  buildBackendEndpoint
} from "../../src/qwen3guard-moderation-adapter/backend";
import { resolveAdapterConfig, type AdapterConfig } from "../../src/qwen3guard-moderation-adapter/config";
import { MAPPING_REVISION } from "../../src/qwen3guard-moderation-adapter/classification";
import {
  createModerationAdapterServer,
  shutdownModerationAdapterServer,
  type AdapterLogRecord
} from "../../src/qwen3guard-moderation-adapter/server";
import { qwenOutputFixtures } from "./fixtures";

const adapterToken = "adapter-contract-token";
const backendToken = "backend-contract-token";
const secretPrompt = "PRIVATE_PROMPT_DO_NOT_LOG";

interface FakeBackend {
  server: Server;
  baseUrl: string;
  calls: number;
  requests: Array<Record<string, unknown>>;
  streams: Record<string, { bytes: number; closed: boolean }>;
  ready: boolean;
  releaseHeld(): void;
}

interface FakeMiniMaxBackend {
  server: Server;
  baseUrl: string;
  requests: Array<Record<string, unknown>>;
}

const openServers: Server[] = [];

afterEach(async () => {
  await Promise.all(
    openServers.splice(0).map(
      (server) =>
        new Promise<void>((resolve) => {
          server.close(() => resolve());
          server.closeAllConnections?.();
        })
    )
  );
});

async function listen(server: Server): Promise<string> {
  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", () => resolve());
  });
  openServers.push(server);
  const address = server.address() as AddressInfo;
  return `http://127.0.0.1:${address.port}`;
}

async function readJsonRequest(request: NodeJS.ReadableStream): Promise<Record<string, unknown>> {
  const chunks: Buffer[] = [];
  for await (const chunk of request) {
    chunks.push(Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk));
  }
  return JSON.parse(Buffer.concat(chunks).toString("utf8")) as Record<string, unknown>;
}

function streamLargeResponse(
  response: import("node:http").ServerResponse,
  observation: { bytes: number; closed: boolean },
  options: { status?: number; contentLength?: number } = {}
): void {
  const targetBytes = 4 * 1024 * 1024;
  response.writeHead(options.status ?? 200, {
    "content-type": "application/json",
    ...(options.contentLength ? { "content-length": String(options.contentLength) } : {})
  });
  response.flushHeaders();
  response.once("close", () => {
    observation.closed = true;
  });

  const pump = () => {
    if (response.destroyed || response.writableEnded) {
      return;
    }
    const chunk = Buffer.alloc(Math.min(64 * 1024, targetBytes - observation.bytes), 120);
    observation.bytes += chunk.byteLength;
    const writable = response.write(chunk);
    if (observation.bytes >= targetBytes) {
      response.end();
      return;
    }
    if (writable) {
      setTimeout(pump, 2);
    } else {
      response.once("drain", () => setTimeout(pump, 2));
    }
  };
  setTimeout(pump, 10);
}

async function startFakeBackend(pathPrefix = ""): Promise<FakeBackend> {
  let calls = 0;
  const requests: Array<Record<string, unknown>> = [];
  const streams: Record<string, { bytes: number; closed: boolean }> = {};
  let ready = true;
  let releaseHold: (() => void) | undefined;
  const hold = new Promise<void>((resolve) => {
    releaseHold = resolve;
  });
  const server = createServer(async (request, response) => {
    if (request.url === `${pathPrefix}/health`) {
      response.writeHead(ready ? 200 : 503, { "content-type": "application/json" });
      response.end(JSON.stringify({ status: ready ? "ready" : "loading" }));
      return;
    }
    if (request.url !== `${pathPrefix}/v1/chat/completions` || request.method !== "POST") {
      response.writeHead(404).end();
      return;
    }
    calls += 1;
    const authorization = request.headers.authorization;
    const body = await readJsonRequest(request);
    requests.push({ ...body, authorization });
    const messages = body.messages as Array<{ content?: string }>;
    const input = messages?.[0]?.content ?? "";
    if (
      input === "stream-oversized" ||
      input === "content-length-oversized" ||
      input === "backend-error-stream"
    ) {
      streams[input] = { bytes: 0, closed: false };
      streamLargeResponse(response, streams[input], {
        status: input === "backend-error-stream" ? 503 : 200,
        contentLength: input === "content-length-oversized" ? 4 * 1024 * 1024 : undefined
      });
      return;
    }
    if (input === "partial-body-stall") {
      streams[input] = { bytes: 0, closed: false };
      response.writeHead(200, { "content-type": "application/json" });
      response.flushHeaders();
      response.once("close", () => {
        streams[input].closed = true;
      });
      const partialBody = Buffer.from('{"choices":[', "utf8");
      streams[input].bytes += partialBody.byteLength;
      response.write(partialBody);
      return;
    }
    if (input === "backend-error") {
      response.writeHead(503, { "content-type": "application/json" });
      response.end(JSON.stringify({ error: "backend unavailable" }));
      return;
    }
    if (input === "hold") {
      await hold;
    }
    if (input === "slow") {
      await new Promise((resolve) => setTimeout(resolve, 250));
    }
    const finishReason =
      input === "truncated" || input === "finish-length"
        ? "length"
        : input === "finish-content-filter"
          ? "content_filter"
          : input === "finish-tool-calls"
            ? "tool_calls"
            : input === "finish-unknown"
              ? "unexpected"
              : input === "finish-missing"
                ? undefined
                : "stop";
    const fixture =
      input === "safe"
        ? qwenOutputFixtures.safe
        : input === "controversial"
          ? qwenOutputFixtures.controversial
          : input === "unsafe"
            ? qwenOutputFixtures.unsafeViolent
            : input === "unknown-category"
              ? qwenOutputFixtures.unsafeUnknown
              : input === "empty"
                ? qwenOutputFixtures.empty
              : input === "unknown-label"
                  ? qwenOutputFixtures.unknownLabel
                  : input === "secret-category"
                    ? "Safety: Unsafe\nCategories: USER_SECRET_123456"
                  : input === "malformed"
                    ? qwenOutputFixtures.malformed
                    : qwenOutputFixtures.safe;
    if (!response.destroyed) {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(
        JSON.stringify({
          id: "chatcmpl_fake",
          choices: [
            {
              message: {
                role: "assistant",
                content: fixture,
                ...(input === "finish-tool-calls" ? { tool_calls: [{ id: "call_fake" }] } : {})
              },
              ...(finishReason === undefined ? {} : { finish_reason: finishReason })
            }
          ]
        })
      );
    }
  });
  const baseUrl = await listen(server);
  return {
    server,
    baseUrl,
    get calls() {
      return calls;
    },
    requests,
    streams,
    get ready() {
      return ready;
    },
    set ready(value: boolean) {
      ready = value;
    },
    releaseHeld() {
      releaseHold?.();
    }
  };
}

async function startFakeMiniMaxBackend(): Promise<FakeMiniMaxBackend> {
  const requests: Array<Record<string, unknown>> = [];
  const server = createServer(async (request, response) => {
    if (request.url === "/v1/models" && request.method === "GET") {
      response.writeHead(200, { "content-type": "application/json" });
      response.end(JSON.stringify({ data: [{ id: "MiniMax-M2.7" }] }));
      return;
    }
    if (request.url !== "/v1/chat/completions" || request.method !== "POST") {
      response.writeHead(404).end();
      return;
    }
    const body = await readJsonRequest(request);
    requests.push({ ...body, authorization: request.headers.authorization });
    const messages = body.messages as Array<{ content?: string }>;
    const input = messages?.[1]?.content ?? "";
    if (input === "auth") {
      response.writeHead(401, { "content-type": "application/json" });
      response.end(JSON.stringify({ base_resp: { status_code: 1004, status_msg: "unauthorized" } }));
      return;
    }
    if (input === "billing") {
      response.writeHead(402, { "content-type": "application/json" });
      response.end(JSON.stringify({ base_resp: { status_code: 1008, status_msg: "insufficient" } }));
      return;
    }
    if (input === "transient") {
      response.writeHead(429, { "content-type": "application/json" });
      response.end(JSON.stringify({ base_resp: { status_code: 1002, status_msg: "rate limited" } }));
      return;
    }
    const sensitive = input === "sensitive";
    const decision = input === "block" ? "block" : "allow";
    response.writeHead(200, { "content-type": "application/json" });
    response.end(JSON.stringify({
      choices: [{
        finish_reason: "stop",
        message: {
          role: "assistant",
          content: sensitive ? "" : JSON.stringify({
            decision,
            category: decision === "allow" ? "none" : "cyber_abuse",
            confidence: 0.99,
            reason_code: decision === "allow" ? "safe" : "actionable_abuse"
          })
        }
      }],
      input_sensitive: sensitive,
      output_sensitive: false,
      base_resp: {
        status_code: sensitive ? 1026 : 0,
        status_msg: sensitive ? "sensitive input" : "success"
      }
    }));
  });
  return { server, baseUrl: await listen(server), requests };
}

function configFor(backendBaseUrl: string, overrides: Record<string, string> = {}): AdapterConfig {
  return resolveAdapterConfig({
    QWEN3GUARD_ADAPTER_BEARER_TOKEN: adapterToken,
    QWEN3GUARD_BACKEND_BASE_URL: backendBaseUrl,
    QWEN3GUARD_BACKEND_MODEL: "Qwen/Qwen3Guard-Gen-0.6B",
    QWEN3GUARD_BACKEND_BEARER_TOKEN: backendToken,
    QWEN3GUARD_MODEL_REVISION: "qwen3guard-test-revision",
    ...overrides
  });
}

function miniMaxConfigFor(backendBaseUrl: string): AdapterConfig {
  return resolveAdapterConfig({
    QWEN3GUARD_ADAPTER_BEARER_TOKEN: adapterToken,
    QWEN3GUARD_BACKEND_PROVIDER: "minimax",
    QWEN3GUARD_BACKEND_BASE_URL: backendBaseUrl,
    QWEN3GUARD_BACKEND_MODEL: "MiniMax-M2.7",
    QWEN3GUARD_BACKEND_BEARER_TOKEN: backendToken,
    QWEN3GUARD_MODEL_REVISION: "minimax-test-revision"
  });
}

async function startAdapter(
  config: AdapterConfig,
  logs: AdapterLogRecord[] = []
): Promise<{ baseUrl: string; logs: AdapterLogRecord[]; server: Server }> {
  const server = createModerationAdapterServer(config, {
    log(record) {
      logs.push(record);
    }
  });
  return { baseUrl: await listen(server), logs, server };
}

async function rawRequest(
  baseUrl: string,
  payload: string,
  options: { end?: boolean; timeoutMs?: number } = {}
): Promise<{ response: string; socket: Socket }> {
  const { hostname, port } = new URL(baseUrl);
  const socket = connect(Number(port), hostname);
  const response = await new Promise<string>((resolve, reject) => {
    let raw = "";
    const timeout = setTimeout(() => {
      socket.destroy();
      reject(new Error("raw request timed out"));
    }, options.timeoutMs ?? 1_000);
    socket.once("error", (error) => {
      clearTimeout(timeout);
      reject(error);
    });
    socket.on("data", (chunk) => {
      raw += chunk.toString("utf8");
      if (raw.includes("\r\n\r\n")) {
        clearTimeout(timeout);
        resolve(raw);
      }
    });
    socket.once("connect", () => {
      if (options.end) {
        socket.end(payload);
      } else {
        socket.write(payload);
      }
    });
  });
  return { response, socket };
}

async function moderate(baseUrl: string, input: unknown, token = adapterToken): Promise<Response> {
  return fetch(`${baseUrl}/v1/moderations`, {
    method: "POST",
    headers: {
      authorization: `Bearer ${token}`,
      "content-type": "application/json",
      "x-request-id": "contract-request-id"
    },
    body: JSON.stringify({ model: "qwen3guard-local", input })
  });
}

describe("Qwen3Guard moderation adapter HTTP contract", () => {
  it("rejects missing or invalid auth without invoking the backend or disclosing credentials", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(configFor(backend.baseUrl));

    const missing = await fetch(`${baseUrl}/v1/moderations`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ model: "qwen3guard-local", input: secretPrompt })
    });
    const wrong = await moderate(baseUrl, secretPrompt, "wrong-token");
    const text = await wrong.text();

    expect(missing.status).toBe(401);
    expect(wrong.status).toBe(401);
    expect(backend.calls).toBe(0);
    expect(text).not.toContain(adapterToken);
    expect(text).not.toContain("wrong-token");
  });

  it.each([
    ["safe", false, "illicit", 0],
    ["controversial", false, "illicit", 0.5],
    ["unsafe", true, "violence", 1],
    ["unknown-category", true, "illicit", 1]
  ])("returns OpenAI-shaped moderation output for %s", async (input, flagged, category, score) => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(configFor(backend.baseUrl));

    const response = await moderate(baseUrl, input);
    const body = await response.json();

    expect(response.status).toBe(200);
    expect(body.model).toBe("qwen3guard-local");
    expect(body.results).toHaveLength(1);
    expect(body.results[0].flagged).toBe(flagged);
    expect(body.results[0].categories[category]).toBe(score > 0);
    expect(body.results[0].category_scores[category]).toBe(score);
  });

  it("normalizes text arrays deterministically and calls the configured backend model with backend auth", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(configFor(backend.baseUrl));

    const response = await moderate(baseUrl, ["first", "second"]);

    expect(response.status).toBe(200);
    expect(backend.requests).toHaveLength(1);
    expect(backend.requests[0].model).toBe("Qwen/Qwen3Guard-Gen-0.6B");
    expect(backend.requests[0].authorization).toBe(`Bearer ${backendToken}`);
    expect(backend.requests[0].messages).toEqual([{ role: "user", content: "first\nsecond" }]);
  });

  it.each([
    ["allow", false],
    ["block", true],
    ["sensitive", true]
  ])("normalizes MiniMax %s into a Moderations decision", async (input, flagged) => {
    const backend = await startFakeMiniMaxBackend();
    const { baseUrl } = await startAdapter(miniMaxConfigFor(backend.baseUrl));

    const response = await moderate(baseUrl, input);
    const body = await response.json();

    expect(response.status).toBe(200);
    expect(body.classifier_policy_revision).toBe("minimax-strict-policy-v5");
    expect(body.results[0].flagged).toBe(flagged);
    expect(backend.requests[0].authorization).toBe(`Bearer ${backendToken}`);
    expect(backend.requests[0].messages).toEqual([
      expect.objectContaining({ role: "system" }),
      { role: "user", content: input }
    ]);
  });

  it.each([
    ["auth", 401, "backend_auth_failed"],
    ["billing", 402, "backend_billing_failed"],
    ["transient", 502, "moderation_backend_error"]
  ])("exposes MiniMax %s failure with deterministic retry semantics", async (input, status, code) => {
    const backend = await startFakeMiniMaxBackend();
    const { baseUrl } = await startAdapter(miniMaxConfigFor(backend.baseUrl));

    const response = await moderate(baseUrl, input);
    const body = await response.json();

    expect(response.status).toBe(status);
    expect(body.error.code).toBe(code);
  });

  it("accepts a bounded gzip-encoded full-context request", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(configFor(backend.baseUrl));
    const transcript = "[system] policy [user] safe";
    const compressed = gzipSync(
      Buffer.from(JSON.stringify({ model: "qwen3guard-local", input: transcript }), "utf8")
    );

    const response = await fetch(`${baseUrl}/v1/moderations`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${adapterToken}`,
        "content-type": "application/json",
        "content-encoding": "gzip"
      },
      body: compressed
    });

    expect(response.status).toBe(200);
    expect(backend.calls).toBe(1);
    expect(backend.requests[0].messages).toEqual([{ role: "user", content: transcript }]);
  });

  it("rejects malformed and decompressed-over-limit gzip bodies without backend work", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(
      configFor(backend.baseUrl, {
        QWEN3GUARD_ADAPTER_MAX_BODY_BYTES: "1024",
        QWEN3GUARD_ADAPTER_MAX_INPUT_CHARS: "10000"
      })
    );
    const headers = {
      authorization: `Bearer ${adapterToken}`,
      "content-type": "application/json",
      "content-encoding": "gzip"
    };

    const malformed = await fetch(`${baseUrl}/v1/moderations`, {
      method: "POST",
      headers,
      body: Buffer.from("not-gzip", "utf8")
    });
    const oversized = await fetch(`${baseUrl}/v1/moderations`, {
      method: "POST",
      headers,
      body: gzipSync(
        Buffer.from(
          JSON.stringify({ model: "qwen3guard-local", input: "x".repeat(2_000) }),
          "utf8"
        )
      )
    });

    expect(malformed.status).toBe(400);
    expect(oversized.status).toBe(413);
    expect(backend.calls).toBe(0);
  });

  it("fails closed for structured image input without invoking the text backend", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(configFor(backend.baseUrl));

    const response = await moderate(baseUrl, [
      { type: "text", text: "associated prompt" },
      { type: "image_url", image_url: { url: "https://invalid.example/image.png" } }
    ]);
    const body = await response.json();

    expect(response.status).toBe(200);
    expect(body.results[0].flagged).toBe(true);
    expect(body.results[0].categories.illicit).toBe(true);
    expect(body.results[0].category_scores.illicit).toBe(1);
    expect(backend.calls).toBe(0);
  });

  it("normalizes structured text input before invoking the text backend", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(configFor(backend.baseUrl));

    const response = await moderate(baseUrl, [{ type: "text", text: "structured text" }]);

    expect(response.status).toBe(200);
    expect(backend.requests).toHaveLength(1);
    expect(backend.requests[0].messages).toEqual([{ role: "user", content: "structured text" }]);
  });

  it("rejects unsupported mixed input without backend work", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(configFor(backend.baseUrl));

    const response = await moderate(baseUrl, ["ok", 42]);

    expect(response.status).toBe(400);
    expect(backend.calls).toBe(0);
  });

  it("strictly rejects invalid JSON, missing model, unknown fields, and non-JSON content", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(configFor(backend.baseUrl));
    const headers = {
      authorization: `Bearer ${adapterToken}`,
      "content-type": "application/json"
    };

    const invalidJson = await fetch(`${baseUrl}/v1/moderations`, {
      method: "POST",
      headers,
      body: "{"
    });
    const missingModel = await fetch(`${baseUrl}/v1/moderations`, {
      method: "POST",
      headers,
      body: JSON.stringify({ input: "safe" })
    });
    const unknownField = await fetch(`${baseUrl}/v1/moderations`, {
      method: "POST",
      headers,
      body: JSON.stringify({ model: "qwen3guard-local", input: "safe", extra: true })
    });
    const nonJson = await fetch(`${baseUrl}/v1/moderations`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${adapterToken}`,
        "content-type": "text/plain"
      },
      body: "safe"
    });

    expect(invalidJson.status).toBe(400);
    expect(missingModel.status).toBe(400);
    expect(unknownField.status).toBe(400);
    expect(nonJson.status).toBe(415);
    expect(backend.calls).toBe(0);
  });

  it.each(["http://[", "/healthz\\suffix", "/v1\\moderations", "/\\evil"])(
    "returns 400 for unsafe request target %s and remains healthy",
    async (rawTarget) => {
      const backend = await startFakeBackend();
      const { baseUrl } = await startAdapter(configFor(backend.baseUrl));
      const { response, socket } = await rawRequest(
        baseUrl,
        `GET ${rawTarget} HTTP/1.1\r\nHost: adapter.local\r\nConnection: close\r\n\r\n`,
        { end: true }
      );
      socket.destroy();

      expect(response).toContain("HTTP/1.1 400");
      expect((await fetch(`${baseUrl}/healthz`)).status).toBe(200);
    }
  );

  it.each(["malformed", "empty", "unknown-label", "truncated"])(
    "returns a visible parse error for %s model output",
    async (input) => {
      const backend = await startFakeBackend();
      const { baseUrl } = await startAdapter(configFor(backend.baseUrl));

      const response = await moderate(baseUrl, input);
      const body = await response.json();

      expect(response.status).toBe(502);
      expect(body.error.code).toBe("qwen_output_parse_error");
    }
  );

  it.each([
    "finish-content-filter",
    "finish-tool-calls",
    "finish-missing",
    "finish-unknown",
    "finish-length"
  ])("rejects non-stop backend finish reason %s as a parse error", async (input) => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(configFor(backend.baseUrl));

    const response = await moderate(baseUrl, input);
    const body = await response.json();

    expect(response.status).toBe(502);
    expect(body.error.code).toBe("qwen_output_parse_error");
  });

  it.each([
    ["stream-oversized", MAX_BACKEND_RESPONSE_BYTES + 256 * 1024],
    ["content-length-oversized", MAX_BACKEND_RESPONSE_BYTES],
    ["backend-error-stream", MAX_BACKEND_RESPONSE_BYTES]
  ] as const)(
    "cancels %s backend responses before consuming an unbounded stream",
    async (input, maximumObservedBytes) => {
      const backend = await startFakeBackend();
      const { baseUrl } = await startAdapter(configFor(backend.baseUrl));

      const response = await moderate(baseUrl, input);

      expect(response.status).toBe(502);
      await expect.poll(() => backend.streams[input]?.closed).toBe(true);
      expect(backend.streams[input].bytes).toBeLessThanOrEqual(maximumObservedBytes);
    }
  );

  it("reports liveness separately from backend readiness", async () => {
    const backend = await startFakeBackend();
    backend.ready = false;
    const { baseUrl } = await startAdapter(configFor(backend.baseUrl));

    const health = await fetch(`${baseUrl}/healthz`);
    const readiness = await fetch(`${baseUrl}/readyz`);
    const notReadyBody = await readiness.json();

    expect(health.status).toBe(200);
    expect(readiness.status).toBe(503);
    expect(notReadyBody.classifier_policy_revision).toBe(MAPPING_REVISION);
    backend.ready = true;
    const ready = await fetch(`${baseUrl}/readyz`);
    expect(ready.status).toBe(200);
    expect((await ready.json()).classifier_policy_revision).toBe(MAPPING_REVISION);
  });

  it("times out bounded inference with 504 and no retry loop", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(
      configFor(backend.baseUrl, { QWEN3GUARD_ADAPTER_INFERENCE_TIMEOUT_MS: "100" })
    );

    const response = await moderate(baseUrl, "slow");
    const metrics = await (await fetch(`${baseUrl}/metrics`)).text();

    expect(response.status).toBe(504);
    expect(backend.calls).toBe(1);
    expect(metrics).toContain('qwen3guard_adapter_requests_total{outcome="timeout"} 1');
  });

  it("classifies a timeout while reading a stalled backend body as timeout", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(
      configFor(backend.baseUrl, { QWEN3GUARD_ADAPTER_INFERENCE_TIMEOUT_MS: "100" })
    );

    const response = await moderate(baseUrl, "partial-body-stall");
    const metrics = await (await fetch(`${baseUrl}/metrics`)).text();

    expect(response.status).toBe(504);
    expect(backend.calls).toBe(1);
    expect(metrics).toContain('qwen3guard_adapter_requests_total{outcome="timeout"} 1');
    expect(metrics).not.toContain(
      'qwen3guard_adapter_requests_total{outcome="backend_error"} 1'
    );
    await expect.poll(() => backend.streams["partial-body-stall"]?.closed).toBe(true);
  });

  it("classifies a caller disconnect during a partial backend stream and releases its slot", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(
      configFor(backend.baseUrl, {
        QWEN3GUARD_ADAPTER_MAX_CONCURRENCY: "1",
        QWEN3GUARD_ADAPTER_MAX_QUEUE: "1",
        QWEN3GUARD_ADAPTER_INFERENCE_TIMEOUT_MS: "1000"
      })
    );
    const controller = new AbortController();
    const disconnected = fetch(`${baseUrl}/v1/moderations`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${adapterToken}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({
        model: "qwen3guard-local",
        input: "partial-body-stall"
      }),
      signal: controller.signal
    });

    await expect.poll(() => backend.streams["partial-body-stall"]?.bytes).toBeGreaterThan(0);
    controller.abort();
    await expect(disconnected).rejects.toThrow();
    await expect.poll(() => backend.streams["partial-body-stall"]?.closed).toBe(true);
    await expect
      .poll(async () =>
        (await (await fetch(`${baseUrl}/metrics`)).text()).includes(
          'qwen3guard_adapter_requests_total{outcome="cancelled"} 1'
        )
      )
      .toBe(true);

    const replacement = await moderate(baseUrl, "safe");

    expect(replacement.status).toBe(200);
    expect(backend.calls).toBe(2);
  });

  it("bounds active inference and queue depth, then emits a retryable overload", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(
      configFor(backend.baseUrl, {
        QWEN3GUARD_ADAPTER_MAX_CONCURRENCY: "1",
        QWEN3GUARD_ADAPTER_MAX_QUEUE: "1",
        QWEN3GUARD_ADAPTER_INFERENCE_TIMEOUT_MS: "1000"
      })
    );

    const active = moderate(baseUrl, "hold");
    await expect.poll(() => backend.calls).toBe(1);
    const queued = moderate(baseUrl, "safe");
    await new Promise((resolve) => setTimeout(resolve, 20));
    const overloaded = await moderate(baseUrl, "safe");
    const metrics = await (await fetch(`${baseUrl}/metrics`)).text();

    expect(overloaded.status).toBe(503);
    expect(overloaded.headers.get("retry-after")).toBe("1");
    expect(metrics).toContain('qwen3guard_adapter_requests_total{outcome="overload"} 1');
    backend.releaseHeld();
    expect((await active).status).toBe(200);
    expect((await queued).status).toBe(200);
    expect(backend.calls).toBe(2);
  });

  it("removes a disconnected queued request so it cannot leave queue pressure behind", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(
      configFor(backend.baseUrl, {
        QWEN3GUARD_ADAPTER_MAX_CONCURRENCY: "1",
        QWEN3GUARD_ADAPTER_MAX_QUEUE: "1",
        QWEN3GUARD_ADAPTER_INFERENCE_TIMEOUT_MS: "1000"
      })
    );

    const active = moderate(baseUrl, "hold");
    await expect.poll(() => backend.calls).toBe(1);
    const controller = new AbortController();
    const queued = fetch(`${baseUrl}/v1/moderations`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${adapterToken}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({ model: "qwen3guard-local", input: "safe" }),
      signal: controller.signal
    });
    await expect
      .poll(async () => (await (await fetch(`${baseUrl}/metrics`)).text()).includes("queue_depth 1"))
      .toBe(true);
    controller.abort();
    await expect(queued).rejects.toThrow();
    await expect
      .poll(async () => (await (await fetch(`${baseUrl}/metrics`)).text()).includes("queue_depth 0"))
      .toBe(true);

    const replacement = moderate(baseUrl, "safe");
    backend.releaseHeld();
    expect((await active).status).toBe(200);
    expect((await replacement).status).toBe(200);
    expect(backend.calls).toBe(2);
  });

  it("enforces body and normalized input limits before inference", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(
      configFor(backend.baseUrl, {
        QWEN3GUARD_ADAPTER_MAX_BODY_BYTES: "1024",
        QWEN3GUARD_ADAPTER_MAX_INPUT_CHARS: "8"
      })
    );

    const inputTooLarge = await moderate(baseUrl, "123456789");
    const bodyTooLarge = await fetch(`${baseUrl}/v1/moderations`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${adapterToken}`,
        "content-type": "application/json"
      },
      body: JSON.stringify({ model: "qwen3guard-local", input: "x".repeat(2_000) })
    });

    expect(inputTooLarge.status).toBe(413);
    expect(bodyTooLarge.status).toBe(413);
    expect(backend.calls).toBe(0);
  });

  it("counts normalized input limits in Unicode code points", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(
      configFor(backend.baseUrl, {
        QWEN3GUARD_ADAPTER_MAX_INPUT_CHARS: "8"
      })
    );

    const atLimit = await moderate(baseUrl, "😀".repeat(8));
    const overLimit = await moderate(baseUrl, "😀".repeat(9));

    expect(atLimit.status).toBe(200);
    expect(overLimit.status).toBe(413);
    expect(backend.calls).toBe(1);
  });

  it("returns 413 and closes an oversized chunked request before the client ends it", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(
      configFor(backend.baseUrl, {
        QWEN3GUARD_ADAPTER_MAX_BODY_BYTES: "1024",
        QWEN3GUARD_ADAPTER_REQUEST_TIMEOUT_MS: "5000"
      })
    );
    const oversizedChunk = "x".repeat(2_048);
    const { response, socket } = await rawRequest(
      baseUrl,
      [
        "POST /v1/moderations HTTP/1.1",
        "Host: adapter.local",
        `Authorization: Bearer ${adapterToken}`,
        "Content-Type: application/json",
        "Transfer-Encoding: chunked",
        "",
        `800\r\n${oversizedChunk}\r\n`
      ].join("\r\n"),
      { timeoutMs: 750 }
    );

    expect(response).toContain("HTTP/1.1 413");
    await expect.poll(() => socket.destroyed).toBe(true);
    expect((await fetch(`${baseUrl}/healthz`)).status).toBe(200);
  });

  it("times out an unfinished request body and closes its connection", async () => {
    const backend = await startFakeBackend();
    const { baseUrl } = await startAdapter(
      configFor(backend.baseUrl, {
        QWEN3GUARD_ADAPTER_MAX_BODY_BYTES: "1024",
        QWEN3GUARD_ADAPTER_REQUEST_TIMEOUT_MS: "100",
        QWEN3GUARD_ADAPTER_HEADERS_TIMEOUT_MS: "100"
      })
    );
    const { response, socket } = await rawRequest(
      baseUrl,
      [
        "POST /v1/moderations HTTP/1.1",
        "Host: adapter.local",
        `Authorization: Bearer ${adapterToken}`,
        "Content-Type: application/json",
        "Transfer-Encoding: chunked",
        "",
        "1\r\n{\r\n"
      ].join("\r\n"),
      { timeoutMs: 1_000 }
    );

    expect(response).toContain("HTTP/1.1 408");
    await expect.poll(() => socket.destroyed).toBe(true);
  });

  it("emits correlatable redacted logs and Prometheus metrics without prompts or credentials", async () => {
    const backend = await startFakeBackend();
    const logs: AdapterLogRecord[] = [];
    const { baseUrl } = await startAdapter(configFor(backend.baseUrl), logs);

    const response = await moderate(baseUrl, secretPrompt);
    const metrics = await (await fetch(`${baseUrl}/metrics`)).text();
    const serializedLogs = JSON.stringify(logs);

    expect(response.status).toBe(200);
    expect(serializedLogs).toContain("contract-request-id");
    expect(serializedLogs).toContain("qwen3guard-test-revision");
    expect(serializedLogs).toContain("qwen3guard-openai-policy-v1");
    expect(serializedLogs).toContain("input_chars");
    expect(serializedLogs).toContain("input_hash");
    expect(serializedLogs).not.toContain(secretPrompt);
    expect(serializedLogs).not.toContain(adapterToken);
    expect(serializedLogs).not.toContain(backendToken);
    expect(metrics).toContain('qwen3guard_adapter_requests_total{outcome="success"} 1');
    expect(metrics).toContain('qwen3guard_adapter_classifications_total{label="Safe"} 1');
    expect(metrics).toContain("qwen3guard_adapter_in_flight 0");
    expect(metrics).toContain("qwen3guard_adapter_queue_depth 0");
  });

  it("logs only normalized category metadata when Qwen returns a secret-like unknown category", async () => {
    const backend = await startFakeBackend();
    const logs: AdapterLogRecord[] = [];
    const { baseUrl } = await startAdapter(configFor(backend.baseUrl), logs);

    const response = await moderate(baseUrl, "secret-category");
    const body = await response.json();
    const serializedLogs = JSON.stringify(logs);

    expect(response.status).toBe(200);
    expect(body.results[0].category_scores.illicit).toBe(1);
    expect(serializedLogs).not.toContain("USER_SECRET_123456");
    expect(logs[0].mapped_categories).toEqual(["illicit"]);
    expect(logs[0].category_mapping).toBe("fallback");
    expect(logs[0].source_category_count).toBe(1);
  });

  it("counts parse and backend failures without returning a synthetic Safe result", async () => {
    const backend = await startFakeBackend();
    const logs: AdapterLogRecord[] = [];
    const { baseUrl } = await startAdapter(configFor(backend.baseUrl), logs);

    expect((await moderate(baseUrl, "malformed")).status).toBe(502);
    expect((await moderate(baseUrl, "backend-error")).status).toBe(502);
    const metrics = await (await fetch(`${baseUrl}/metrics`)).text();

    expect(metrics).toContain('qwen3guard_adapter_requests_total{outcome="parse_error"} 1');
    expect(metrics).toContain('qwen3guard_adapter_requests_total{outcome="backend_error"} 1');
    expect(logs.map((record) => record.error_class)).toEqual(["parse_error", "backend_error"]);
  });

  it("cancels active moderation and closes gracefully within the shutdown deadline", async () => {
    const backend = await startFakeBackend();
    const logs: AdapterLogRecord[] = [];
    const { baseUrl, server } = await startAdapter(configFor(backend.baseUrl), logs);
    const active = moderate(baseUrl, "hold");
    await expect.poll(() => backend.calls).toBe(1);

    const result = await shutdownModerationAdapterServer(server, 500, (record) => {
      logs.push(record);
    });
    const response = await active;
    backend.releaseHeld();

    expect(result.status).toBe("graceful");
    expect(response.status).toBe(503);
    expect(logs.some((record) => record.event === "qwen3guard_adapter.stopping")).toBe(true);
  });

  it("forces open connections closed and records a shutdown timeout", async () => {
    const logs: AdapterLogRecord[] = [];
    let requestStarted = false;
    const server = createServer(() => {
      requestStarted = true;
      // This deliberately uncooperative handler proves the hard shutdown deadline.
    });
    const baseUrl = await listen(server);
    const pendingRequest = fetch(baseUrl).catch(() => undefined);
    await expect.poll(() => requestStarted).toBe(true);

    const result = await shutdownModerationAdapterServer(server, 100, (record) => {
      logs.push(record);
    });
    await pendingRequest;

    expect(result.status).toBe("forced");
    expect(logs.some((record) => record.event === "qwen3guard_adapter.shutdown_timeout")).toBe(
      true
    );
  });
});
