/**
 * [INPUT]: Fake MiniMax HTTP responses and provider-aware adapter configuration.
 * [OUTPUT]: Assertions for request shape, fail-closed classifier output, and typed provider failures.
 * [POS]: Direct backend-client contract tests below the adapter HTTP boundary.
 *
 * [PROTOCOL]:
 * 1. Update this header when MiniMax backend transport behavior changes.
 * 2. Update this folder's .folder.md when this file changes.
 */

import { describe, expect, it, vi } from "vitest";

import {
  createBackendClient,
  type FetchImplementation
} from "../../src/qwen3guard-moderation-adapter/backend";
import { resolveAdapterConfig } from "../../src/qwen3guard-moderation-adapter/config";

function config() {
  return resolveAdapterConfig({
    QWEN3GUARD_ADAPTER_BEARER_TOKEN: "adapter-token",
    QWEN3GUARD_BACKEND_PROVIDER: "minimax",
    QWEN3GUARD_BACKEND_BASE_URL: "https://api.minimaxi.com",
    QWEN3GUARD_BACKEND_MODEL: "MiniMax-M3",
    QWEN3GUARD_MINIMAX_SERVICE_TIER: "priority",
    QWEN3GUARD_BACKEND_BEARER_TOKEN: "minimax-token"
  });
}

function miniMaxResponse(body: Record<string, unknown>, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" }
  });
}

function successfulBody(content: string, overrides: Record<string, unknown> = {}) {
  return {
    choices: [{ finish_reason: "stop", message: { role: "assistant", content } }],
    input_sensitive: false,
    output_sensitive: false,
    base_resp: { status_code: 0, status_msg: "success" },
    ...overrides
  };
}

describe("MiniMax moderation backend client", () => {
  it("sends a bounded non-streaming classifier request with separated trust roles", async () => {
    const fetchImpl = vi.fn<FetchImplementation>(async (_url, init) => {
      const body = JSON.parse(String(init?.body)) as Record<string, unknown>;
      const messages = body.messages as Array<Record<string, unknown>>;
      expect(body.model).toBe("MiniMax-M3");
      expect(body.stream).toBe(false);
      expect(body.max_completion_tokens).toBe(128);
      expect(body.service_tier).toBe("priority");
      expect(body.thinking).toEqual({ type: "disabled" });
      expect(body.reasoning_split).toBeUndefined();
      expect(messages[0].role).toBe("system");
      expect(messages[1]).toEqual({ role: "user", content: "dangerous transcript" });
      expect(new Headers(init?.headers).get("authorization")).toBe("Bearer minimax-token");
      return miniMaxResponse(successfulBody(
        '{"decision":"allow","category":"none","confidence":0.99,"reason_code":"safe"}'
      ));
    });
    const client = createBackendClient(config(), fetchImpl);

    const result = await client.classify("dangerous transcript", new AbortController().signal);

    expect(result.flagged).toBe(false);
    expect(fetchImpl).toHaveBeenCalledWith(
      "https://api.minimaxi.com/v1/chat/completions",
      expect.objectContaining({ method: "POST" })
    );
  });

  it.each([
    ["input_sensitive", { input_sensitive: true }],
    ["output_sensitive", { output_sensitive: true }],
    ["1026", { base_resp: { status_code: 1026, status_msg: "sensitive input" } }],
    ["1027", { base_resp: { status_code: 1027, status_msg: "sensitive output" } }]
  ])("maps %s to a successful blocked classification", async (_name, overrides) => {
    const fetchImpl = vi.fn<FetchImplementation>(async () =>
      miniMaxResponse(successfulBody("", overrides))
    );
    const client = createBackendClient(config(), fetchImpl);

    const result = await client.classify("input", new AbortController().signal);

    expect(result.flagged).toBe(true);
    expect(result.categories.illicit).toBe(true);
  });

  it.each([
    [1004, "auth", 401],
    [1008, "billing", 402],
    [1002, "backend", 429]
  ])("maps provider code %s to %s", async (providerCode, kind, status) => {
    const fetchImpl = vi.fn<FetchImplementation>(async () =>
      miniMaxResponse({ base_resp: { status_code: providerCode, status_msg: "provider error" } }, status)
    );
    const client = createBackendClient(config(), fetchImpl);

    await expect(client.classify("input", new AbortController().signal)).rejects.toMatchObject({
      kind,
      providerCode
    });
  });

  it.each([
    ["missing choice", successfulBody("", { choices: [] })],
    ["non-stop finish", {
      ...successfulBody('{"decision":"allow","category":"none","confidence":1,"reason_code":"safe"}'),
      choices: [{ finish_reason: "length", message: { role: "assistant", content: "{}" } }]
    }],
    ["empty content", successfulBody("")],
    ["malformed final output", successfulBody("not-json")]
  ])("maps %s to a successful blocked classification", async (_name, body) => {
    const fetchImpl = vi.fn<FetchImplementation>(async () => miniMaxResponse(body));
    const client = createBackendClient(config(), fetchImpl);

    const result = await client.classify("input", new AbortController().signal);

    expect(result.flagged).toBe(true);
    expect(result.categories.illicit).toBe(true);
  });

  it("checks MiniMax readiness through the authenticated models endpoint", async () => {
    const fetchImpl = vi.fn<FetchImplementation>(async () => miniMaxResponse({ data: [] }));
    const client = createBackendClient(config(), fetchImpl);

    expect(await client.checkReadiness()).toBe(true);
    expect(fetchImpl).toHaveBeenCalledWith(
      "https://api.minimaxi.com/v1/models",
      expect.objectContaining({ method: "GET" })
    );
  });
});
