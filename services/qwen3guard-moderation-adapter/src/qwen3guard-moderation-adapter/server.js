/**
 * [INPUT]: Validated adapter config, authenticated HTTP requests, and Qwen backend responses.
 * [OUTPUT]: Raw-target-safe bounded HTTP/gzip surfaces plus cancellable hard-deadline shutdown.
 * [POS]: Checked-in JavaScript moderation boundary, separate from Omni routing.
 *
 * [PROTOCOL]:
 * 1. Update this header when HTTP server responsibilities change.
 * 2. Update this folder's .folder.md when this file changes.
 */

import { createHash, randomUUID, timingSafeEqual } from "node:crypto";
import { createServer } from "node:http";
import { gunzipSync } from "node:zlib";

import { BackendClientError, QwenBackendClient } from "./backend.js";
import { MAPPING_REVISION } from "./classification.js";
import { AdapterMetrics } from "./metrics.js";

class RequestValidationError extends Error {
  constructor(status, code, message, closeConnection = false) {
    super(message);
    this.status = status;
    this.code = code;
    this.closeConnection = closeConnection;
  }
}

class OverloadError extends Error {}
class CancelledRequestError extends Error {}

class InferenceScheduler {
  constructor(maxConcurrency, maxQueue, metrics) {
    this.maxConcurrency = maxConcurrency;
    this.maxQueue = maxQueue;
    this.metrics = metrics;
    this.active = 0;
    this.queue = [];
    this.report();
  }

  run(task, signal) {
    if (signal.aborted) {
      return Promise.reject(new CancelledRequestError());
    }
    return new Promise((resolve, reject) => {
      const entry = { task, signal, resolve, reject };
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

  start(entry) {
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

  drain() {
    while (this.active < this.maxConcurrency && this.queue.length > 0) {
      const next = this.queue.shift();
      if (next) {
        this.start(next);
      }
    }
    this.report();
  }

  report() {
    this.metrics.setScheduler(this.active, this.queue.length);
  }
}

function requestId(request) {
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

function authorized(request, secret) {
  const supplied =
    typeof request.headers.authorization === "string" ? request.headers.authorization : "";
  const expectedDigest = createHash("sha256").update(`Bearer ${secret}`).digest();
  const suppliedDigest = createHash("sha256").update(supplied).digest();
  return timingSafeEqual(expectedDigest, suppliedDigest);
}

function writeJson(response, status, payload, correlationId, headers = {}) {
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

function writeError(response, status, code, message, correlationId, headers = {}) {
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

function readBody(request, maxBytes, timeoutMs, signal) {
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
    const chunks = [];
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
    const fail = (error, pause = false) => {
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
    const onData = (chunk) => {
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
    const onError = (error) => fail(error);
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

function decodeRequestBody(request, rawBody, maxBytes) {
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
    if (error && typeof error === "object" && error.code === "ERR_BUFFER_TOO_LARGE") {
      throw new RequestValidationError(
        413,
        "request_body_too_large",
        "decompressed request body exceeds configured limit"
      );
    }
    throw new RequestValidationError(400, "invalid_gzip", "request body is not valid gzip");
  }
}

function moderationInput(rawBody, maxInputChars) {
  let parsed;
  try {
    parsed = JSON.parse(rawBody);
  } catch {
    throw new RequestValidationError(400, "invalid_json", "request body must be valid JSON");
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new RequestValidationError(400, "invalid_request", "request body must be a JSON object");
  }
  if (Object.keys(parsed).some((key) => key !== "model" && key !== "input")) {
    throw new RequestValidationError(400, "invalid_request", "request contains unsupported fields");
  }
  if (typeof parsed.model !== "string" || parsed.model.trim() === "") {
    throw new RequestValidationError(400, "invalid_model", "model must be a non-empty string");
  }
  let input;
  if (typeof parsed.input === "string") {
    input = parsed.input;
  } else if (
    Array.isArray(parsed.input) &&
    parsed.input.length > 0 &&
    parsed.input.every((item) => typeof item === "string")
  ) {
    input = parsed.input.join("\n");
  } else {
    throw new RequestValidationError(
      400,
      "unsupported_input",
      "input must be text or a non-empty text array"
    );
  }
  if (input.trim() === "") {
    throw new RequestValidationError(400, "invalid_input", "input must contain text");
  }
  if (input.length > maxInputChars) {
    throw new RequestValidationError(
      413,
      "input_too_large",
      "normalized input exceeds configured limit"
    );
  }
  return { model: parsed.model, input };
}

function outcomeFor(error) {
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

function parseRequestPath(rawTarget) {
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
  let parsed;
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

function closeRequestAfterResponse(request, response) {
  const close = () => request.destroy();
  response.once("finish", close);
  response.once("close", close);
}

function safeOperationalLog(log, record) {
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

const runtimeStates = new WeakMap();

export async function shutdownModerationAdapterServer(
  server,
  timeoutMs,
  log = (record) => console.info(JSON.stringify(record))
) {
  runtimeStates.get(server)?.beginShutdown();
  server.closeIdleConnections?.();

  return new Promise((resolve) => {
    let settled = false;
    const finish = (status) => {
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

export function createModerationAdapterServer(config, dependencies = {}) {
  const metrics = new AdapterMetrics();
  const backend = new QwenBackendClient(config, dependencies.fetch ?? fetch);
  const scheduler = new InferenceScheduler(config.maxConcurrency, config.maxQueue, metrics);
  const log = dependencies.log ?? ((record) => console.info(JSON.stringify(record)));
  const emit = (record) =>
    safeOperationalLog(log, { ...record, timestamp: new Date().toISOString() });
  const activeControllers = new Set();
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

  const handleRequest = async (request, response) => {
    const correlationId = requestId(request);
    let path;
    try {
      path = parseRequestPath(request.url);
    } catch (error) {
      const validation =
        error instanceof RequestValidationError
          ? error
          : new RequestValidationError(
              400,
              "invalid_request_target",
              "request target is invalid",
              true
            );
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
      writeJson(response, ready ? 200 : 503, { status: ready ? "ready" : "not_ready" });
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

    let normalized;
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
        model_revision: config.modelRevision,
        mapping_revision: MAPPING_REVISION,
        input_chars: normalized.input.length,
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
          model_revision: config.modelRevision,
          mapping_revision: MAPPING_REVISION,
          input_chars: normalized.input.length,
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
        model_revision: config.modelRevision,
        mapping_revision: MAPPING_REVISION,
        input_chars: normalized.input.length,
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
      if (backendError.kind === "parse") {
        writeError(
          response,
          502,
          "qwen_output_parse_error",
          "model output could not be parsed",
          correlationId
        );
        return;
      }
      writeError(
        response,
        502,
        "qwen_backend_error",
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
