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
import { MINIMAX_CLASSIFIER_INSTRUCTION } from "../../src/qwen3guard-moderation-adapter/minimax";

function config(overrides: Record<string, string> = {}) {
  return resolveAdapterConfig({
    QWEN3GUARD_ADAPTER_BEARER_TOKEN: "adapter-token",
    QWEN3GUARD_BACKEND_PROVIDER: "minimax",
    QWEN3GUARD_BACKEND_BASE_URL: "https://api.minimaxi.com",
    QWEN3GUARD_BACKEND_MODEL: "MiniMax-M3",
    QWEN3GUARD_MINIMAX_SERVICE_TIER: "priority",
    QWEN3GUARD_BACKEND_BEARER_TOKEN: "minimax-token",
    ...overrides
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
      expect(messages).toHaveLength(2);
      expect(messages[0]).toEqual({
        role: "system",
        content: MINIMAX_CLASSIFIER_INSTRUCTION
      });
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
    ["numeric error code", {
      error: {
        type: "rate_limit_error",
        code: 2056,
        message: "Token Plan usage limit exceeded"
      }
    }, 2056],
    ["message suffix", {
      error: {
        type: "rate_limit_error",
        code: null,
        message: "Token Plan usage limit exceeded (2056)"
      }
    }, 2056],
    ["inactive subscription", {
      error: {
        type: "invalid_request_error",
        code: null,
        message: "No active token plan subscription (2062)"
      }
    }, 2062]
  ])("maps Token Plan quota failure from %s to deterministic billing", async (
    _name,
    body,
    providerCode
  ) => {
    const fetchImpl = vi.fn<FetchImplementation>(async () => miniMaxResponse(body, 429));
    const client = createBackendClient(config(), fetchImpl);

    await expect(client.classify("input", new AbortController().signal)).rejects.toMatchObject({
      kind: "billing",
      providerCode
    });
  });

  it("keeps an unclassified HTTP 429 transient", async () => {
    const fetchImpl = vi.fn<FetchImplementation>(async () => miniMaxResponse({
      error: {
        type: "rate_limit_error",
        code: null,
        message: "Too many requests"
      }
    }, 429));
    const client = createBackendClient(config(), fetchImpl);

    await expect(client.classify("input", new AbortController().signal)).rejects.toMatchObject({
      kind: "backend",
      providerCode: undefined
    });
  });

  it.each([
    ["rate limit", 429, 1002],
    ["upstream timeout", 504, 1001],
    ["parameter error", 400, 2013]
  ])("preserves %s provider failures below the adapter HTTP boundary", async (
    _name,
    status,
    providerCode
  ) => {
    const fetchImpl = vi.fn<FetchImplementation>(async () => miniMaxResponse({
      base_resp: { status_code: providerCode, status_msg: "provider failure" }
    }, status));
    const client = createBackendClient(config(), fetchImpl);

    await expect(client.classify("input", new AbortController().signal)).rejects.toMatchObject({
      kind: "backend",
      providerCode
    });
  });

  it.each([
    ["non-JSON response", new Response("not-json", { status: 502 })],
    ["non-object JSON response", new Response("[]", { status: 502 })],
    ["empty non-2xx response", new Response("", { status: 503 })]
  ])("classifies %s as a backend transport failure", async (_name, response) => {
    const fetchImpl = vi.fn<FetchImplementation>(async () => response);
    const client = createBackendClient(config(), fetchImpl);

    await expect(client.classify("input", new AbortController().signal)).rejects.toMatchObject({
      kind: "backend"
    });
  });

  it("classifies a failed fetch as a network-level backend failure", async () => {
    const fetchImpl = vi.fn<FetchImplementation>(async () => {
      throw new TypeError("fetch failed");
    });
    const client = createBackendClient(config(), fetchImpl);

    await expect(client.classify("input", new AbortController().signal)).rejects.toMatchObject({
      kind: "backend",
      message: "MiniMax backend request failed"
    });
  });

  it("distinguishes its own inference timeout from a generic backend failure", async () => {
    const fetchImpl = vi.fn<FetchImplementation>(async (_url, init) =>
      await new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")), {
          once: true
        });
      })
    );
    const client = createBackendClient(config({
      QWEN3GUARD_ADAPTER_INFERENCE_TIMEOUT_MS: "100"
    }), fetchImpl);

    await expect(client.classify("input", new AbortController().signal)).rejects.toMatchObject({
      kind: "timeout"
    });
  });

  it("distinguishes caller cancellation from a generic backend failure", async () => {
    const fetchImpl = vi.fn<FetchImplementation>(async (_url, init) =>
      await new Promise<Response>((_resolve, reject) => {
        init?.signal?.addEventListener("abort", () => reject(new DOMException("aborted", "AbortError")), {
          once: true
        });
      })
    );
    const client = createBackendClient(config(), fetchImpl);
    const caller = new AbortController();

    const result = client.classify("input", caller.signal);
    caller.abort();

    await expect(result).rejects.toMatchObject({ kind: "cancelled" });
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

  it("uses the model list only as a connectivity and configured-model readiness proof", async () => {
    const fetchImpl = vi.fn<FetchImplementation>(async () => miniMaxResponse({
      data: [{ id: "MiniMax-M3" }]
    }));
    const client = createBackendClient(config(), fetchImpl);

    expect(await client.checkReadiness()).toBe(true);
    expect(fetchImpl).toHaveBeenCalledWith(
      "https://api.minimaxi.com/v1/models",
      expect.objectContaining({ method: "GET" })
    );
    expect(fetchImpl).toHaveBeenCalledTimes(1);
  });

  it.each([
    ["provider-level error", {
      data: [{ id: "MiniMax-M3" }],
      base_resp: { status_code: 1004, status_msg: "unauthorized" }
    }],
    ["configured model is unavailable", {
      data: [{ id: "MiniMax-M2.7-highspeed" }],
      base_resp: { status_code: 0, status_msg: "success" }
    }],
    ["malformed model list", { data: [{}] }]
  ])("does not report readiness when %s", async (_name, body) => {
    const fetchImpl = vi.fn<FetchImplementation>(async () => miniMaxResponse(body));
    const client = createBackendClient(config(), fetchImpl);

    expect(await client.checkReadiness()).toBe(false);
  });
});
