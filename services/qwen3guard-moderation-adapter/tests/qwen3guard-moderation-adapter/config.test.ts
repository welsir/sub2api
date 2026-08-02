/**
 * [INPUT]: Environment maps for the standalone Qwen3Guard moderation adapter.
 * [OUTPUT]: Assertions for strict secrets, contained backend paths, and bounded deadlines.
 * [POS]: Configuration schema contract coverage for adapter startup.
 *
 * [PROTOCOL]:
 * 1. Update this header when configuration behavior changes.
 * 2. Update this folder's .folder.md when this file changes.
 */

import { describe, expect, it, vi } from "vitest";

import {
  QwenBackendClient,
  buildBackendEndpoint
} from "../../src/qwen3guard-moderation-adapter/backend";
import { resolveAdapterConfig } from "../../src/qwen3guard-moderation-adapter/config";

const requiredEnv = {
  QWEN3GUARD_ADAPTER_BEARER_TOKEN: "adapter-test-token",
  QWEN3GUARD_BACKEND_BASE_URL: "http://127.0.0.1:8101",
  QWEN3GUARD_BACKEND_MODEL: "Qwen/Qwen3Guard-Gen-0.6B"
};

describe("resolveAdapterConfig", () => {
  it("requires external adapter credentials and backend identity", () => {
    expect(() => resolveAdapterConfig({})).toThrow(/QWEN3GUARD_ADAPTER_BEARER_TOKEN/);
    expect(() =>
      resolveAdapterConfig({ QWEN3GUARD_ADAPTER_BEARER_TOKEN: "token" })
    ).toThrow(/QWEN3GUARD_BACKEND_BASE_URL/);
  });

  it("applies bounded loopback defaults", () => {
    const config = resolveAdapterConfig(requiredEnv);

    expect(config.backendProvider).toBe("qwen");
    expect(config.backendReadinessPath).toBe("/health");
    expect(config.host).toBe("127.0.0.1");
    expect(config.port).toBe(8090);
    expect(config.maxInputChars).toBeGreaterThan(0);
    expect(config.maxBodyBytes).toBeGreaterThan(config.maxInputChars);
    expect(config.maxConcurrency).toBeGreaterThan(0);
    expect(config.maxQueue).toBeGreaterThanOrEqual(0);
    expect(config.inferenceTimeoutMs).toBeGreaterThan(0);
    expect(config.requestTimeoutMs).toBeGreaterThan(0);
    expect(config.headersTimeoutMs).toBeGreaterThan(0);
    expect(config.headersTimeoutMs).toBeLessThanOrEqual(config.requestTimeoutMs);
    expect(config.shutdownTimeoutMs).toBeGreaterThan(0);
    expect(config.backendBaseUrl).toBe("http://127.0.0.1:8101");
  });

  it("accepts MiniMax and selects its provider-compatible readiness path", () => {
    const config = resolveAdapterConfig({
      ...requiredEnv,
      QWEN3GUARD_BACKEND_PROVIDER: "minimax",
      QWEN3GUARD_BACKEND_BASE_URL: "https://api.minimaxi.com"
    });

    expect(config.backendProvider).toBe("minimax");
    expect(config.backendReadinessPath).toBe("/v1/models");
  });

  it("rejects unsupported moderation backend providers", () => {
    expect(() =>
      resolveAdapterConfig({
        ...requiredEnv,
        QWEN3GUARD_BACKEND_PROVIDER: "unknown"
      })
    ).toThrow(/BACKEND_PROVIDER.*qwen.*minimax/i);
  });

  it("rejects invalid URLs and non-positive or fractional limits", () => {
    expect(() =>
      resolveAdapterConfig({ ...requiredEnv, QWEN3GUARD_BACKEND_BASE_URL: "file:///tmp/model" })
    ).toThrow(/http/);
    expect(() =>
      resolveAdapterConfig({ ...requiredEnv, QWEN3GUARD_ADAPTER_MAX_CONCURRENCY: "0" })
    ).toThrow(/MAX_CONCURRENCY/);
    expect(() =>
      resolveAdapterConfig({ ...requiredEnv, QWEN3GUARD_ADAPTER_MAX_QUEUE: "1.5" })
    ).toThrow(/MAX_QUEUE/);
    expect(() =>
      resolveAdapterConfig({
        ...requiredEnv,
        QWEN3GUARD_BACKEND_BASE_URL: "http://127.0.0.1:8101?token=secret"
      })
    ).toThrow(/query|hash/);
    expect(() =>
      resolveAdapterConfig({
        ...requiredEnv,
        QWEN3GUARD_BACKEND_BASE_URL: "http://127.0.0.1:8101#fragment"
      })
    ).toThrow(/query|hash/);
    expect(() =>
      resolveAdapterConfig({
        ...requiredEnv,
        QWEN3GUARD_ADAPTER_REQUEST_TIMEOUT_MS: "100",
        QWEN3GUARD_ADAPTER_HEADERS_TIMEOUT_MS: "200"
      })
    ).toThrow(/HEADERS_TIMEOUT/);
  });

  it("appends readiness and chat paths beneath a fixed backend path prefix", () => {
    expect(buildBackendEndpoint("http://trusted/qwen/", "/health")).toBe(
      "http://trusted/qwen/health"
    );
    expect(buildBackendEndpoint("http://trusted/qwen/", "/v1/chat/completions")).toBe(
      "http://trusted/qwen/v1/chat/completions"
    );
  });

  it.each([
    "/\\\\evil.example/health",
    "/../admin",
    "/%2e%2e/admin",
    "/.%2e/admin",
    "/%2e./admin",
    "/%2E%2E/admin"
  ])("rejects unsafe backend endpoint path %s in config and URL construction", (unsafePath) => {
    expect(() =>
      resolveAdapterConfig({
        ...requiredEnv,
        QWEN3GUARD_BACKEND_READINESS_PATH: unsafePath
      })
    ).toThrow(/READINESS_PATH/);
    expect(() => buildBackendEndpoint("http://trusted/qwen/", unsafePath)).toThrow(
      /backend endpoint path/i
    );
  });

  it.each([
    "http://trusted/qwen\\@evil.example/",
    "http://trusted/qwen/../admin",
    "http://trusted/qwen/%2e%2e/admin"
  ])("rejects unsafe backend base path %s before normalization", (unsafeBaseUrl) => {
    expect(() =>
      resolveAdapterConfig({
        ...requiredEnv,
        QWEN3GUARD_BACKEND_BASE_URL: unsafeBaseUrl
      })
    ).toThrow(/BACKEND_BASE_URL/);
    expect(() => buildBackendEndpoint(unsafeBaseUrl, "/health")).toThrow(
      /backend base URL/i
    );
  });

  it("does not invoke fetch when an unsafe endpoint could redirect backend auth", async () => {
    const fetchImpl = vi.fn<typeof fetch>();
    const client = new QwenBackendClient(
      {
        ...resolveAdapterConfig(requiredEnv),
        backendBearerToken: "backend-test-token",
        backendReadinessPath: "/\\\\evil.example/health"
      },
      fetchImpl
    );

    expect(await client.checkReadiness()).toBe(false);
    expect(fetchImpl).not.toHaveBeenCalled();
  });
});
