/**
 * [INPUT]: Validated text, provider-aware backend configuration, and caller cancellation signal.
 * [OUTPUT]: Origin-contained classifications, fail-closed MiniMax uncertainty, and typed transport failures.
 * [POS]: Cancellation-aware Chat Completions clients behind the moderation contract.
 *
 * [PROTOCOL]:
 * 1. Update this header when backend client responsibilities change.
 * 2. Update this folder's .folder.md when this file changes.
 */

import {
  normalizeBackendBaseUrl,
  validateBackendEndpointPath,
  type AdapterConfig
} from "./config";
import {
  parseAndMapClassification,
  type MappedClassification
} from "./classification";
import {
  buildMiniMaxChatRequest,
  mapMiniMaxSensitiveResult,
  mapMiniMaxUncertainResult,
  parseMiniMaxClassification
} from "./minimax";

export type BackendErrorKind = "timeout" | "backend" | "parse" | "cancelled" | "auth" | "billing";
export const MAX_BACKEND_RESPONSE_BYTES = 1_048_576;

export class BackendClientError extends Error {
  constructor(
    readonly kind: BackendErrorKind,
    message: string,
    readonly providerCode?: number
  ) {
    super(message);
    this.name = "BackendClientError";
  }
}

export type FetchImplementation = typeof fetch;

export interface ModerationBackendClient {
  checkReadiness(callerSignal?: AbortSignal): Promise<boolean>;
  classify(input: string, callerSignal: AbortSignal): Promise<MappedClassification>;
}

export function buildBackendEndpoint(baseUrl: string, endpointPath: string): string {
  validateBackendEndpointPath(endpointPath);
  const normalizedBaseUrl = normalizeBackendBaseUrl(baseUrl);
  const base = new URL(`${normalizedBaseUrl.replace(/\/+$/, "")}/`);
  const basePathPrefix = base.pathname;
  const endpoint = new URL(endpointPath.slice(1), base);
  if (
    endpoint.origin !== base.origin ||
    !endpoint.pathname.startsWith(basePathPrefix)
  ) {
    throw new Error("backend endpoint path escaped the configured base URL");
  }
  return endpoint.toString();
}

async function cancelResponseBody(response: Response): Promise<void> {
  try {
    await response.body?.cancel();
  } catch {
    // The transport may already have closed the body.
  }
}

async function readLimitedResponseBody(
  response: Response,
  signal: AbortSignal
): Promise<string> {
  const rawContentLength = response.headers.get("content-length");
  if (rawContentLength && /^\d+$/.test(rawContentLength)) {
    const contentLength = Number(rawContentLength);
    if (contentLength > MAX_BACKEND_RESPONSE_BYTES) {
      await cancelResponseBody(response);
      throw new BackendClientError("backend", "Qwen backend response exceeded 1 MiB");
    }
  }
  if (!response.body) {
    return "";
  }

  const reader = response.body.getReader();
  const chunks: Buffer[] = [];
  let totalBytes = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) {
        break;
      }
      totalBytes += value.byteLength;
      if (totalBytes > MAX_BACKEND_RESPONSE_BYTES) {
        await reader.cancel();
        throw new BackendClientError("backend", "Qwen backend response exceeded 1 MiB");
      }
      chunks.push(Buffer.from(value));
    }
  } catch (error) {
    if (error instanceof BackendClientError) {
      throw error;
    }
    if (signal.aborted) {
      throw error;
    }
    try {
      await reader.cancel();
    } catch {
      // The transport may already have closed the reader.
    }
    throw new BackendClientError("backend", "Qwen backend response stream failed");
  }
  return Buffer.concat(chunks, totalBytes).toString("utf8");
}

export class QwenBackendClient {
  constructor(
    private readonly config: AdapterConfig,
    private readonly fetchImpl: FetchImplementation = fetch
  ) {}

  private headers(includeJson = false): Record<string, string> {
    return {
      ...(includeJson ? { "content-type": "application/json" } : {}),
      ...(this.config.backendBearerToken
        ? { authorization: `Bearer ${this.config.backendBearerToken}` }
        : {})
    };
  }

  async checkReadiness(callerSignal?: AbortSignal): Promise<boolean> {
    const controller = new AbortController();
    const forwardAbort = () => controller.abort();
    callerSignal?.addEventListener("abort", forwardAbort, { once: true });
    const timeout = setTimeout(() => controller.abort(), this.config.readinessTimeoutMs);
    try {
      const response = await this.fetchImpl(
        buildBackendEndpoint(this.config.backendBaseUrl, this.config.backendReadinessPath),
        {
          method: "GET",
          headers: this.headers(),
          signal: controller.signal
        }
      );
      const ready = response.ok;
      await cancelResponseBody(response);
      return ready;
    } catch {
      return false;
    } finally {
      clearTimeout(timeout);
      callerSignal?.removeEventListener("abort", forwardAbort);
    }
  }

  async classify(input: string, callerSignal: AbortSignal): Promise<MappedClassification> {
    const controller = new AbortController();
    let timedOut = false;
    const forwardAbort = () => controller.abort();
    callerSignal.addEventListener("abort", forwardAbort, { once: true });
    const timeout = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, this.config.inferenceTimeoutMs);

    try {
      const response = await this.fetchImpl(buildBackendEndpoint(
        this.config.backendBaseUrl,
        "/v1/chat/completions"
      ), {
        method: "POST",
        headers: this.headers(true),
        signal: controller.signal,
        body: JSON.stringify({
          model: this.config.backendModel,
          messages: [{ role: "user", content: input }],
          temperature: 0,
          max_tokens: 128,
          stream: false
        })
      });
      if (!response.ok) {
        await cancelResponseBody(response);
        throw new BackendClientError("backend", `Qwen backend returned HTTP ${response.status}`);
      }

      const rawResponse = await readLimitedResponseBody(response, controller.signal);
      let body: unknown;
      try {
        body = JSON.parse(rawResponse);
      } catch {
        throw new BackendClientError("backend", "Qwen backend returned invalid JSON");
      }
      const choice =
        body && typeof body === "object" && Array.isArray((body as { choices?: unknown }).choices)
          ? (body as { choices: Array<Record<string, unknown>> }).choices[0]
          : undefined;
      if (!choice || choice.finish_reason !== "stop") {
        throw new BackendClientError("parse", "Qwen output did not finish with stop");
      }
      const message =
        choice.message && typeof choice.message === "object"
          ? (choice.message as Record<string, unknown>)
          : undefined;
      const content = message?.content;
      if (typeof content !== "string" || content.length === 0) {
        throw new BackendClientError("parse", "Qwen output was empty or missing");
      }
      try {
        return parseAndMapClassification(content);
      } catch {
        throw new BackendClientError("parse", "Qwen output did not match the supported format");
      }
    } catch (error) {
      if (timedOut) {
        throw new BackendClientError("timeout", "Qwen inference deadline exceeded");
      }
      if (callerSignal.aborted) {
        throw new BackendClientError("cancelled", "moderation request was cancelled");
      }
      if (error instanceof BackendClientError) {
        throw error;
      }
      throw new BackendClientError("backend", "Qwen backend request failed");
    } finally {
      clearTimeout(timeout);
      callerSignal.removeEventListener("abort", forwardAbort);
    }
  }
}

function miniMaxProviderCode(body: unknown): number | undefined {
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    return undefined;
  }
  const baseResponse = (body as { base_resp?: unknown }).base_resp;
  if (!baseResponse || typeof baseResponse !== "object" || Array.isArray(baseResponse)) {
    return undefined;
  }
  const code = (baseResponse as { status_code?: unknown }).status_code;
  return typeof code === "number" && Number.isSafeInteger(code) ? code : undefined;
}

function parseMiniMaxBody(rawResponse: string): Record<string, unknown> {
  let body: unknown;
  try {
    body = JSON.parse(rawResponse);
  } catch {
    throw new BackendClientError("backend", "MiniMax backend returned invalid JSON");
  }
  if (!body || typeof body !== "object" || Array.isArray(body)) {
    throw new BackendClientError("backend", "MiniMax backend returned an invalid body");
  }
  return body as Record<string, unknown>;
}

function miniMaxFailure(httpStatus: number, providerCode?: number): BackendClientError {
  if (providerCode === 1004 || httpStatus === 401 || httpStatus === 403) {
    return new BackendClientError("auth", "MiniMax backend authentication failed", providerCode);
  }
  if (providerCode === 1008 || httpStatus === 402) {
    return new BackendClientError("billing", "MiniMax backend balance is insufficient", providerCode);
  }
  return new BackendClientError("backend", "MiniMax backend request failed", providerCode);
}

function miniMaxSensitive(body: Record<string, unknown>, providerCode?: number): boolean {
  return body.input_sensitive === true ||
    body.output_sensitive === true ||
    providerCode === 1026 ||
    providerCode === 1027;
}

export class MiniMaxBackendClient implements ModerationBackendClient {
  constructor(
    private readonly config: AdapterConfig,
    private readonly fetchImpl: FetchImplementation = fetch
  ) {}

  private headers(includeJson = false): Record<string, string> {
    return {
      ...(includeJson ? { "content-type": "application/json" } : {}),
      ...(this.config.backendBearerToken
        ? { authorization: `Bearer ${this.config.backendBearerToken}` }
        : {})
    };
  }

  async checkReadiness(callerSignal?: AbortSignal): Promise<boolean> {
    const controller = new AbortController();
    const forwardAbort = () => controller.abort();
    callerSignal?.addEventListener("abort", forwardAbort, { once: true });
    const timeout = setTimeout(() => controller.abort(), this.config.readinessTimeoutMs);
    try {
      const response = await this.fetchImpl(
        buildBackendEndpoint(this.config.backendBaseUrl, this.config.backendReadinessPath),
        { method: "GET", headers: this.headers(), signal: controller.signal }
      );
      const ready = response.ok;
      await cancelResponseBody(response);
      return ready;
    } catch {
      return false;
    } finally {
      clearTimeout(timeout);
      callerSignal?.removeEventListener("abort", forwardAbort);
    }
  }

  async classify(input: string, callerSignal: AbortSignal): Promise<MappedClassification> {
    const controller = new AbortController();
    let timedOut = false;
    const forwardAbort = () => controller.abort();
    callerSignal.addEventListener("abort", forwardAbort, { once: true });
    const timeout = setTimeout(() => {
      timedOut = true;
      controller.abort();
    }, this.config.inferenceTimeoutMs);

    try {
      const response = await this.fetchImpl(
        buildBackendEndpoint(this.config.backendBaseUrl, "/v1/chat/completions"),
        {
          method: "POST",
          headers: this.headers(true),
          signal: controller.signal,
          body: JSON.stringify(buildMiniMaxChatRequest(
            this.config.backendModel,
            input,
            this.config.miniMaxServiceTier
          ))
        }
      );
      const rawResponse = await readLimitedResponseBody(response, controller.signal);
      const body = parseMiniMaxBody(rawResponse);
      const providerCode = miniMaxProviderCode(body);
      if (miniMaxSensitive(body, providerCode)) {
        return mapMiniMaxSensitiveResult(String(providerCode ?? "sensitive"));
      }
      if (!response.ok || (providerCode !== undefined && providerCode !== 0)) {
        throw miniMaxFailure(response.status, providerCode);
      }
      const choices = Array.isArray(body.choices) ? body.choices : [];
      const choice = choices[0];
      if (!choice || typeof choice !== "object" || Array.isArray(choice)) {
        return mapMiniMaxUncertainResult("missing_choice");
      }
      const choiceRecord = choice as Record<string, unknown>;
      if (choiceRecord.finish_reason !== "stop") {
        return mapMiniMaxUncertainResult("non_stop_finish");
      }
      const message = choiceRecord.message;
      const content = message && typeof message === "object" && !Array.isArray(message)
        ? (message as Record<string, unknown>).content
        : undefined;
      if (typeof content !== "string" || content.trim() === "") {
        return mapMiniMaxUncertainResult("empty_content");
      }
      try {
        return parseMiniMaxClassification(content);
      } catch {
        return mapMiniMaxUncertainResult("malformed_classifier_output");
      }
    } catch (error) {
      if (timedOut) {
        throw new BackendClientError("timeout", "MiniMax inference deadline exceeded");
      }
      if (callerSignal.aborted) {
        throw new BackendClientError("cancelled", "moderation request was cancelled");
      }
      if (error instanceof BackendClientError) {
        throw error;
      }
      throw new BackendClientError("backend", "MiniMax backend request failed");
    } finally {
      clearTimeout(timeout);
      callerSignal.removeEventListener("abort", forwardAbort);
    }
  }
}

export function createBackendClient(
  config: AdapterConfig,
  fetchImpl: FetchImplementation = fetch
): ModerationBackendClient {
  return config.backendProvider === "minimax"
    ? new MiniMaxBackendClient(config, fetchImpl)
    : new QwenBackendClient(config, fetchImpl);
}
