/**
 * [INPUT]: Validated adapter config, authenticated HTTP requests, Unicode text limits, and provider backend responses.
 * [OUTPUT]: Code-point-bounded moderation plus classifier-revision metadata, deterministic image fail-closed decisions, and hard-deadline shutdown.
 * [POS]: Standalone moderation process boundary, separate from Omni northbound routing.
 *
 * [PROTOCOL]:
 * 1. Update this header when HTTP server responsibilities change.
 * 2. Update this folder's .folder.md when this file changes.
 */

import { createHash, randomUUID, timingSafeEqual } from "node:crypto";
import { createServer, type IncomingMessage, type Server, type ServerResponse } from "node:http";
import { gunzipSync } from "node:zlib";

import {
  BackendClientError,
  createBackendClient,
  type FetchImplementation
} from "./backend.js";
import {
  MAPPING_REVISION,
  evaluatedCategories,
  type MappedClassification
} from "./classification.js";
import type { AdapterConfig } from "./config.js";
import { AdapterMetrics, type RequestOutcome } from "./metrics.js";
import { MINIMAX_CLASSIFIER_POLICY_REVISION } from "./minimax.js";

export interface AdapterLogRecord {
  event: string;
  timestamp: string;
  [key: string]: unknown;
}

export interface AdapterServerDependencies {
  fetch?: FetchImplementation;
  log?: (record: AdapterLogRecord) => void;
}

class RequestValidationError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly closeConnection = false
  ) {
    super(message);
  }
}

class OverloadError extends Error {}
class CancelledRequestError extends Error {}

interface QueueEntry {
  task: () => Promise<MappedClassification>;
  signal: AbortSignal;
  resolve: (result: MappedClassification) => void;
  reject: (error: unknown) => void;
  onAbort?: () => void;
}

class InferenceScheduler {
  private active = 0;
  private readonly queue: QueueEntry[] = [];

  constructor(
    private readonly maxConcurrency: number,
    private readonly maxQueue: number,
    private readonly metrics: AdapterMetrics
  ) {
    this.report();
  }

  run(
    task: () => Promise<MappedClassification>,
    signal: AbortSignal
  ): Promise<MappedClassification> {
    if (signal.aborted) {
      return Promise.reject(new CancelledRequestError());
    }
    return new Promise((resolve, reject) => {
      const entry: QueueEntry = { task, signal, resolve, reject };
      if (this.active < this.maxConcurrency) {
        this.start(entry);
        return;
      }
      if (this.queue.length >= this.maxQueue) {
        reject(new OverloadError());
        return;
      }
      entry.onAbort = () => {
        const index = this.queue.indexOf(entry);
        if (index >= 0) {
          this.queue.splice(index, 1);
          this.report();
        }
        reject(new CancelledRequestError());
      };
      signal.addEventListener("abort", entry.onAbort, { once: true });
      this.queue.push(entry);
      this.report();
    });
  }

  private start(entry: QueueEntry): void {
    if (entry.onAbort) {
      entry.signal.removeEventListener("abort", entry.onAbort);
    }
    if (entry.signal.aborted) {
      entry.reject(new CancelledRequestError());
      this.drain();
      return;
    }
    this.active += 1;
    this.report();
    entry
      .task()
      .then(entry.resolve, entry.reject)
      .finally(() => {
        this.active -= 1;
        this.drain();
      });
  }

  private drain(): void {
    while (this.active < this.maxConcurrency && this.queue.length > 0) {
      const next = this.queue.shift();
      if (next) {
        this.start(next);
      }
    }
    this.report();
  }

  private report(): void {
    this.metrics.setScheduler(this.active, this.queue.length);
  }
}

function requestId(request: IncomingMessage): string {
  const value = request.headers["x-request-id"];
  if (
    typeof value === "string" &&
    value.length <= 128 &&
    /^[A-Za-z0-9._:-]+$/.test(value)
  ) {
    return value;
  }
  return randomUUID();
}

function authorized(request: IncomingMessage, secret: string): boolean {
  const supplied = typeof request.headers.authorization === "string"
    ? request.headers.authorization
    : "";
  const expectedDigest = createHash("sha256").update(`Bearer ${secret}`).digest();
  const suppliedDigest = createHash("sha256").update(supplied).digest();
  return timingSafeEqual(expectedDigest, suppliedDigest);
}

function writeJson(
  response: ServerResponse,
  status: number,
  payload: unknown,
  correlationId?: string,
  headers: Record<string, string> = {}
): void {
  if (response.destroyed || response.writableEnded) {
    return;
  }
  response.writeHead(status, {
    "content-type": "application/json; charset=utf-8",
    "cache-control": "no-store",
    ...(correlationId ? { "x-request-id": correlationId } : {}),
    ...headers
  });
  response.end(JSON.stringify(payload));
}

function writeError(
  response: ServerResponse,
  status: number,
  code: string,
  message: string,
  correlationId: string,
  headers: Record<string, string> = {}
): void {
  writeJson(
    response,
    status,
    {
      error: {
        type: status >= 500 ? "server_error" : "invalid_request_error",
        code,
        message,
        request_id: correlationId
      }
    },
    correlationId,
    headers
  );
}

function readBody(
  request: IncomingMessage,
  maxBytes: number,
  timeoutMs: number,
  signal: AbortSignal
): Promise<Buffer> {
  const declared = Number(request.headers["content-length"]);
  if (Number.isFinite(declared) && declared > maxBytes) {
    request.pause();
    return Promise.reject(
      new RequestValidationError(
        413,
        "request_body_too_large",
        "request body exceeds configured limit",
        true
      )
    );
  }
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    let total = 0;
    let settled = false;
    const cleanup = () => {
      clearTimeout(timeout);
      request.removeListener("data", onData);
      request.removeListener("end", onEnd);
      request.removeListener("aborted", onAborted);
      request.removeListener("error", onError);
      signal.removeEventListener("abort", onSignalAbort);
    };
    const fail = (error: unknown, pause = false) => {
      if (settled) {
        return;
      }
      settled = true;
      cleanup();
      if (pause) {
        request.pause();
      }
      reject(error);
    };
    const onData = (chunk: Buffer | string) => {
      const buffer = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
      total += buffer.byteLength;
      if (total > maxBytes) {
        chunks.length = 0;
        fail(
          new RequestValidationError(
            413,
            "request_body_too_large",
            "request body exceeds configured limit",
            true
          ),
          true
        );
        return;
      }
      chunks.push(buffer);
    };
    const onEnd = () => {
      if (settled) {
        return;
      }
      settled = true;
      cleanup();
      resolve(Buffer.concat(chunks));
    };
    const onAborted = () => fail(new CancelledRequestError());
    const onError = (error: Error) => fail(error);
    const onSignalAbort = () => fail(new CancelledRequestError(), true);
    const timeout = setTimeout(
      () =>
        fail(
          new RequestValidationError(
            408,
            "request_timeout",
            "request body deadline exceeded",
            true
          ),
          true
        ),
      timeoutMs
    );

    request.on("data", onData);
    request.once("end", onEnd);
    request.once("aborted", onAborted);
    request.once("error", onError);
    signal.addEventListener("abort", onSignalAbort, { once: true });
  });
}

function decodeRequestBody(request: IncomingMessage, rawBody: Buffer, maxBytes: number): string {
  const rawEncoding = request.headers["content-encoding"];
  const encoding = (typeof rawEncoding === "string" ? rawEncoding : "").trim().toLowerCase();
  if (encoding === "" || encoding === "identity") {
    return rawBody.toString("utf8");
  }
  if (encoding !== "gzip") {
    throw new RequestValidationError(
      415,
      "unsupported_content_encoding",
      "content-encoding must be gzip or identity"
    );
  }
  try {
    return gunzipSync(rawBody, { maxOutputLength: maxBytes }).toString("utf8");
  } catch (error) {
    const code = typeof error === "object" && error !== null && "code" in error
      ? String((error as { code?: unknown }).code ?? "")
      : "";
    if (code === "ERR_BUFFER_TOO_LARGE") {
      throw new RequestValidationError(
        413,
        "request_body_too_large",
        "decompressed request body exceeds configured limit"
      );
    }
    throw new RequestValidationError(400, "invalid_gzip", "request body is not valid gzip");
  }
}

const LOCAL_MEDIA_MAPPING_REVISION = "local-text-only-media-fail-closed-v1";

function localMediaBlockClassification(): MappedClassification {
  const categoryScores = Object.fromEntries(
    evaluatedCategories.map((category) => [category, category === "illicit" ? 1 : 0])
  ) as MappedClassification["categoryScores"];
  const categories = Object.fromEntries(
    evaluatedCategories.map((category) => [category, categoryScores[category] > 0])
  ) as MappedClassification["categories"];
  return {
    label: "Unsafe",
    sourceCategoryCount: 1,
    mappedCategories: ["illicit"],
    categoryMapping: "mapped",
    flagged: true,
    policyValue: 1,
    categories,
    categoryScores,
    mappingRevision: LOCAL_MEDIA_MAPPING_REVISION
  };
}

function moderationInput(rawBody: string, maxInputChars: number): {
  model: string;
  input: string;
  inputChars: number;
  hasImage: boolean;
} {
  let parsed: unknown;
  try {
    parsed = JSON.parse(rawBody);
  } catch {
    throw new RequestValidationError(400, "invalid_json", "request body must be valid JSON");
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new RequestValidationError(400, "invalid_request", "request body must be a JSON object");
  }
  const body = parsed as Record<string, unknown>;
  if (Object.keys(body).some((key) => key !== "model" && key !== "input")) {
    throw new RequestValidationError(400, "invalid_request", "request contains unsupported fields");
  }
  if (typeof body.model !== "string" || body.model.trim() === "") {
    throw new RequestValidationError(400, "invalid_model", "model must be a non-empty string");
  }
  let input: string;
  let hasImage = false;
  if (typeof body.input === "string") {
    input = body.input;
  } else if (
    Array.isArray(body.input) &&
    body.input.length > 0 &&
    body.input.every((item) => typeof item === "string")
  ) {
    input = body.input.join("\n");
  } else if (Array.isArray(body.input) && body.input.length > 0) {
    const textParts: string[] = [];
    for (const item of body.input) {
      if (!item || typeof item !== "object" || Array.isArray(item)) {
        throw new RequestValidationError(
          400,
          "unsupported_input",
          "structured input must contain text or image_url parts"
        );
      }
      const part = item as Record<string, unknown>;
      if (part.type === "text" && typeof part.text === "string") {
        if (part.text.trim() !== "") {
          textParts.push(part.text);
        }
        continue;
      }
      const imageUrl = part.image_url;
      if (
        part.type === "image_url" &&
        imageUrl &&
        typeof imageUrl === "object" &&
        !Array.isArray(imageUrl) &&
        typeof (imageUrl as Record<string, unknown>).url === "string" &&
        String((imageUrl as Record<string, unknown>).url).trim() !== ""
      ) {
        hasImage = true;
        continue;
      }
      throw new RequestValidationError(
        400,
        "unsupported_input",
        "structured input must contain text or image_url parts"
      );
    }
    input = textParts.join("\n");
  } else {
    throw new RequestValidationError(
      400,
      "unsupported_input",
      "input must be text or a non-empty text array"
    );
  }
  if (input.trim() === "" && !hasImage) {
    throw new RequestValidationError(400, "invalid_input", "input must contain text");
  }
  if (input.trim() === "" && hasImage) {
    input = "[IMAGE_ATTACHMENT_PRESENT]";
  }
  let inputChars = 0;
  for (const _codePoint of input) {
    inputChars += 1;
  }
  if (inputChars > maxInputChars) {
    throw new RequestValidationError(
      413,
      "input_too_large",
      "normalized input exceeds configured limit"
    );
  }
  return { model: body.model, input, inputChars, hasImage };
}

function outcomeFor(error: BackendClientError): RequestOutcome {
  if (error.kind === "timeout") {
    return "timeout";
  }
  if (error.kind === "parse") {
    return "parse_error";
  }
  if (error.kind === "cancelled") {
    return "cancelled";
  }
  return "backend_error";
}

function parseRequestPath(rawTarget: string | undefined): string {
  if (
    !rawTarget ||
    !rawTarget.startsWith("/") ||
    rawTarget.startsWith("//") ||
    rawTarget.includes("\\")
  ) {
    throw new RequestValidationError(
      400,
      "invalid_request_target",
      "request target must use origin form",
      true
    );
  }
  const baseUrl = new URL("http://adapter.local");
  let parsed: URL;
  try {
    parsed = new URL(rawTarget, baseUrl);
  } catch {
    throw new RequestValidationError(
      400,
      "invalid_request_target",
      "request target is invalid",
      true
    );
  }
  if (parsed.origin !== baseUrl.origin) {
    throw new RequestValidationError(
      400,
      "invalid_request_target",
      "request target is invalid",
      true
    );
  }
  return parsed.pathname;
}

function closeRequestAfterResponse(request: IncomingMessage, response: ServerResponse): void {
  const close = () => request.destroy();
  response.once("finish", close);
  response.once("close", close);
}

function safeOperationalLog(
  log: (record: AdapterLogRecord) => void,
  record: AdapterLogRecord
): void {
  try {
    log(record);
  } catch {
    try {
      console.error(
        JSON.stringify({
          event: "qwen3guard_adapter.log_error",
          timestamp: new Date().toISOString(),
          error_class: "log_sink_error"
        })
      );
    } catch {
      // Logging must never escape a request or shutdown boundary.
    }
  }
}

interface AdapterRuntimeState {
  beginShutdown(): void;
}

export interface AdapterShutdownResult {
  status: "graceful" | "forced";
}

const runtimeStates = new WeakMap<Server, AdapterRuntimeState>();

export async function shutdownModerationAdapterServer(
  server: Server,
  timeoutMs: number,
  log: (record: AdapterLogRecord) => void = (record) => console.info(JSON.stringify(record))
): Promise<AdapterShutdownResult> {
  runtimeStates.get(server)?.beginShutdown();
  server.closeIdleConnections?.();

  return new Promise((resolve) => {
    let settled = false;
    const finish = (status: "graceful" | "forced") => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(deadline);
      resolve({ status });
    };
    const deadline = setTimeout(() => {
      safeOperationalLog(log, {
        event: "qwen3guard_adapter.shutdown_timeout",
        timestamp: new Date().toISOString(),
        timeout_ms: timeoutMs
      });
      server.closeAllConnections?.();
      finish("forced");
    }, timeoutMs);

    try {
      server.close((error) => {
        if (error) {
          safeOperationalLog(log, {
            event: "qwen3guard_adapter.shutdown_error",
            timestamp: new Date().toISOString(),
            error_class: "server_close_error"
          });
          server.closeAllConnections?.();
          finish("forced");
          return;
        }
        finish("graceful");
      });
    } catch {
      server.closeAllConnections?.();
      finish("forced");
    }
  });
}

export function createModerationAdapterServer(
  config: AdapterConfig,
  dependencies: AdapterServerDependencies = {}
): Server {
  const metrics = new AdapterMetrics();
  const backend = createBackendClient(config, dependencies.fetch ?? fetch);
  const configuredClassifierPolicyRevision = config.backendProvider === "minimax"
    ? MINIMAX_CLASSIFIER_POLICY_REVISION
    : MAPPING_REVISION;
  const scheduler = new InferenceScheduler(config.maxConcurrency, config.maxQueue, metrics);
  const log = dependencies.log ?? ((record: AdapterLogRecord) => console.info(JSON.stringify(record)));
  const emit = (record: { event: string; [key: string]: unknown }) =>
    safeOperationalLog(log, { ...record, timestamp: new Date().toISOString() });
  const activeControllers = new Set<AbortController>();
  let shuttingDown = false;
  const beginShutdown = () => {
    if (shuttingDown) {
      return;
    }
    shuttingDown = true;
    emit({ event: "qwen3guard_adapter.stopping" });
    for (const controller of activeControllers) {
      controller.abort();
    }
  };

  const handleRequest = async (request: IncomingMessage, response: ServerResponse) => {
    const correlationId = requestId(request);
    let path: string;
    try {
      path = parseRequestPath(request.url);
    } catch (error) {
      const validation =
        error instanceof RequestValidationError
          ? error
          : new RequestValidationError(400, "invalid_request_target", "request target is invalid", true);
      closeRequestAfterResponse(request, response);
      writeError(
        response,
        validation.status,
        validation.code,
        validation.message,
        correlationId,
        { connection: "close" }
      );
      return;
    }

    if (shuttingDown) {
      closeRequestAfterResponse(request, response);
      writeError(
        response,
        503,
        "server_shutting_down",
        "moderation adapter is shutting down",
        correlationId,
        { connection: "close", "retry-after": "1" }
      );
      return;
    }

    const cancellation = new AbortController();
    activeControllers.add(cancellation);
    const release = () => activeControllers.delete(cancellation);
    request.once("aborted", () => cancellation.abort());
    response.once("finish", release);
    response.once("close", () => {
      if (!response.writableEnded) {
        cancellation.abort();
      }
      release();
    });

    if (request.method === "GET" && path === "/healthz") {
      writeJson(response, 200, { status: "alive" });
      return;
    }
    if (request.method === "GET" && path === "/readyz") {
      const ready = await backend.checkReadiness(cancellation.signal);
      if (shuttingDown) {
        closeRequestAfterResponse(request, response);
        writeError(
          response,
          503,
          "server_shutting_down",
          "moderation adapter is shutting down",
          correlationId,
          { connection: "close", "retry-after": "1" }
        );
        return;
      }
      writeJson(response, ready ? 200 : 503, {
        status: ready ? "ready" : "not_ready",
        classifier_policy_revision: configuredClassifierPolicyRevision
      });
      return;
    }
    if (request.method === "GET" && path === "/metrics") {
      response.writeHead(200, { "content-type": "text/plain; version=0.0.4; charset=utf-8" });
      response.end(metrics.render());
      return;
    }
    if (path !== "/v1/moderations") {
      writeJson(response, 404, { error: { code: "not_found", message: "not found" } });
      return;
    }
    if (request.method !== "POST") {
      writeJson(
        response,
        405,
        { error: { code: "method_not_allowed", message: "method not allowed" } },
        undefined,
        { allow: "POST" }
      );
      return;
    }

    if (!authorized(request, config.adapterBearerToken)) {
      request.resume();
      metrics.incrementRequest("auth_error");
      emit({
        event: "qwen3guard_moderation.rejected",
        request_id: correlationId,
        error_class: "auth_error"
      });
      writeError(response, 401, "invalid_authentication", "invalid authentication", correlationId);
      return;
    }
    if (!request.headers["content-type"]?.toLowerCase().startsWith("application/json")) {
      request.resume();
      metrics.incrementRequest("invalid_request");
      writeError(
        response,
        415,
        "unsupported_content_type",
        "content-type must be application/json",
        correlationId
      );
      return;
    }

    let normalized: { model: string; input: string; inputChars: number; hasImage: boolean };
    try {
      const rawBody = await readBody(
          request,
          config.maxBodyBytes,
          config.requestTimeoutMs,
          cancellation.signal
        );
      normalized = moderationInput(
        decodeRequestBody(request, rawBody, config.maxBodyBytes),
        config.maxInputChars
      );
    } catch (error) {
      if (error instanceof CancelledRequestError) {
        metrics.incrementRequest("cancelled");
        if (shuttingDown) {
          closeRequestAfterResponse(request, response);
          writeError(
            response,
            503,
            "server_shutting_down",
            "moderation adapter is shutting down",
            correlationId,
            { connection: "close", "retry-after": "1" }
          );
        }
        return;
      }
      const validation =
        error instanceof RequestValidationError
          ? error
          : new RequestValidationError(400, "invalid_request", "invalid request");
      metrics.incrementRequest("invalid_request");
      emit({
        event: "qwen3guard_moderation.rejected",
        request_id: correlationId,
        error_class: "invalid_request",
        error_code: validation.code
      });
      if (validation.closeConnection) {
        closeRequestAfterResponse(request, response);
      }
      writeError(
        response,
        validation.status,
        validation.code,
        validation.message,
        correlationId,
        validation.closeConnection ? { connection: "close" } : {}
      );
      return;
    }

    const inputHash = createHash("sha256").update(normalized.input).digest("hex");
    if (normalized.hasImage) {
      const result = localMediaBlockClassification();
      metrics.incrementRequest("success");
      metrics.incrementClassification(result.label);
      emit({
        event: "qwen3guard_moderation.completed",
        request_id: correlationId,
        backend_provider: "local_policy",
        model_revision: "text-only-image-fail-closed",
        mapping_revision: result.mappingRevision,
        input_chars: normalized.inputChars,
        input_hash: inputHash,
        label: result.label,
        mapped_categories: result.mappedCategories,
        category_mapping: result.categoryMapping,
        source_category_count: result.sourceCategoryCount,
        latency_ms: 0
      });
      writeJson(
        response,
        200,
        {
          id: `modr_${randomUUID()}`,
          model: normalized.model,
          classifier_policy_revision: result.mappingRevision,
          results: [
            {
              flagged: result.flagged,
              categories: result.categories,
              category_scores: result.categoryScores
            }
          ]
        },
        correlationId
      );
      return;
    }
    const started = Date.now();
    try {
      const result = await scheduler.run(
        () => backend.classify(normalized.input, cancellation.signal),
        cancellation.signal
      );
      const latencyMs = Date.now() - started;
      metrics.incrementRequest("success");
      metrics.incrementClassification(result.label);
      metrics.observeLatency(latencyMs);
      emit({
        event: "qwen3guard_moderation.completed",
        request_id: correlationId,
        backend_provider: config.backendProvider,
        model_revision: config.modelRevision,
        mapping_revision: result.mappingRevision,
        input_chars: normalized.inputChars,
        input_hash: inputHash,
        label: result.label,
        mapped_categories: result.mappedCategories,
        category_mapping: result.categoryMapping,
        source_category_count: result.sourceCategoryCount,
        latency_ms: latencyMs
      });
      writeJson(
        response,
        200,
        {
          id: `modr_${randomUUID()}`,
          model: normalized.model,
          classifier_policy_revision: result.mappingRevision,
          results: [
            {
              flagged: result.flagged,
              categories: result.categories,
              category_scores: result.categoryScores
            }
          ]
        },
        correlationId
      );
    } catch (error) {
      const latencyMs = Date.now() - started;
      if (error instanceof OverloadError) {
        metrics.incrementRequest("overload");
        emit({
          event: "qwen3guard_moderation.overload",
          request_id: correlationId,
          backend_provider: config.backendProvider,
          model_revision: config.modelRevision,
          mapping_revision: configuredClassifierPolicyRevision,
          input_chars: normalized.inputChars,
          input_hash: inputHash,
          latency_ms: latencyMs
        });
        writeError(
          response,
          503,
          "adapter_overloaded",
          "moderation adapter is overloaded",
          correlationId,
          { "retry-after": "1" }
        );
        return;
      }
      if (error instanceof CancelledRequestError) {
        metrics.incrementRequest("cancelled");
        if (shuttingDown) {
          closeRequestAfterResponse(request, response);
          writeError(
            response,
            503,
            "server_shutting_down",
            "moderation adapter is shutting down",
            correlationId,
            { connection: "close", "retry-after": "1" }
          );
        }
        return;
      }
      const backendError =
        error instanceof BackendClientError
          ? error
          : new BackendClientError("backend", "unexpected backend failure");
      const outcome = outcomeFor(backendError);
      metrics.incrementRequest(outcome);
      metrics.observeLatency(latencyMs);
      emit({
        event: "qwen3guard_moderation.error",
        request_id: correlationId,
        backend_provider: config.backendProvider,
        model_revision: config.modelRevision,
        mapping_revision: configuredClassifierPolicyRevision,
        input_chars: normalized.inputChars,
        input_hash: inputHash,
        latency_ms: latencyMs,
        error_class: outcome
      });
      if (backendError.kind === "cancelled") {
        if (shuttingDown) {
          closeRequestAfterResponse(request, response);
          writeError(
            response,
            503,
            "server_shutting_down",
            "moderation adapter is shutting down",
            correlationId,
            { connection: "close", "retry-after": "1" }
          );
        }
        return;
      }
      if (backendError.kind === "timeout") {
        writeError(
          response,
          504,
          "inference_timeout",
          "moderation inference timed out",
          correlationId
        );
        return;
      }
      if (backendError.kind === "auth") {
        writeError(
          response,
          401,
          "backend_auth_failed",
          "moderation backend authentication failed",
          correlationId
        );
        return;
      }
      if (backendError.kind === "billing") {
        writeError(
          response,
          402,
          "backend_billing_failed",
          "moderation backend billing is unavailable",
          correlationId
        );
        return;
      }
      if (backendError.kind === "parse") {
        writeError(
          response,
          502,
          config.backendProvider === "qwen" ? "qwen_output_parse_error" : "model_output_parse_error",
          "model output could not be parsed",
          correlationId
        );
        return;
      }
      writeError(
        response,
        502,
        "moderation_backend_error",
        "model backend request failed",
        correlationId
      );
    }
  };

  const server = createServer((request, response) => {
    void handleRequest(request, response).catch(() => {
      const correlationId = requestId(request);
      metrics.incrementRequest("backend_error");
      emit({
        event: "qwen3guard_moderation.error",
        request_id: correlationId,
        error_class: "unexpected_request_error"
      });
      request.pause();
      closeRequestAfterResponse(request, response);
      writeError(
        response,
        500,
        "internal_error",
        "unexpected moderation adapter error",
        correlationId,
        { connection: "close" }
      );
    });
  });

  server.requestTimeout = config.requestTimeoutMs;
  server.headersTimeout = config.headersTimeoutMs;
  server.keepAliveTimeout = Math.min(5_000, config.requestTimeoutMs);
  server.on("clientError", (_error, socket) => {
    emit({
      event: "qwen3guard_adapter.request_parse_error",
      error_class: "http_parse_error"
    });
    if (socket.writable) {
      socket.end(
        "HTTP/1.1 400 Bad Request\r\nConnection: close\r\nContent-Length: 0\r\n\r\n"
      );
      return;
    }
    socket.destroy();
  });
  runtimeStates.set(server, { beginShutdown });
  return server;
}
