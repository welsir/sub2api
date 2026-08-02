/**
 * [INPUT]: Process environment variables for adapter and provider backend settings.
 * [OUTPUT]: Validated provider-aware configuration with contained backend paths and externalized secrets.
 * [POS]: Checked-in JavaScript startup configuration for the standalone adapter.
 *
 * [PROTOCOL]:
 * 1. Update this header when configuration responsibilities change.
 * 2. Update this folder's .folder.md when this file changes.
 */

function requiredText(env, name) {
  const value = env[name]?.trim();
  if (!value) {
    throw new Error(`${name} is required`);
  }
  return value;
}

function integerSetting(env, name, defaultValue, minimum, maximum) {
  const raw = env[name]?.trim();
  const value = raw === undefined || raw === "" ? defaultValue : Number(raw);
  if (!Number.isSafeInteger(value) || value < minimum || value > maximum) {
    throw new Error(`${name} must be an integer from ${minimum} to ${maximum}`);
  }
  return value;
}

function repeatedlyDecodedPath(rawPath, settingName) {
  let decoded = rawPath;
  for (let index = 0; index <= rawPath.length; index += 1) {
    let next;
    try {
      next = decodeURIComponent(decoded);
    } catch {
      throw new Error(`${settingName} must use valid percent encoding`);
    }
    if (next === decoded) {
      return decoded;
    }
    decoded = next;
  }
  throw new Error(`${settingName} contains excessive nested encoding`);
}

export function validateBackendEndpointPath(
  rawPath,
  settingName = "backend endpoint path"
) {
  if (
    !rawPath.startsWith("/") ||
    rawPath.startsWith("//") ||
    rawPath.includes("\\")
  ) {
    throw new Error(`${settingName} must start with one slash and contain no backslashes`);
  }
  const decoded = repeatedlyDecodedPath(rawPath, settingName);
  if (decoded.includes("\\") || decoded.includes("?") || decoded.includes("#")) {
    throw new Error(`${settingName} contains an unsafe path character`);
  }
  if (decoded.split("/").some((segment) => segment === "." || segment === "..")) {
    throw new Error(`${settingName} must not contain dot segments`);
  }
}

function rawAbsoluteUrlPath(rawUrl) {
  const schemeEnd = rawUrl.indexOf("://");
  const pathStart = rawUrl.indexOf("/", schemeEnd + 3);
  if (pathStart < 0) {
    return "/";
  }
  const queryStart = rawUrl.indexOf("?", pathStart);
  const hashStart = rawUrl.indexOf("#", pathStart);
  const candidates = [queryStart, hashStart].filter((index) => index >= 0);
  const pathEnd = candidates.length > 0 ? Math.min(...candidates) : rawUrl.length;
  return rawUrl.slice(pathStart, pathEnd);
}

export function normalizeBackendBaseUrl(
  raw,
  settingName = "backend base URL"
) {
  if (raw.includes("\\")) {
    throw new Error(`${settingName} must not contain backslashes`);
  }
  let parsed;
  try {
    parsed = new URL(raw);
  } catch {
    throw new Error(`${settingName} must be an http(s) URL`);
  }
  if (
    (parsed.protocol !== "http:" && parsed.protocol !== "https:") ||
    parsed.username ||
    parsed.password
  ) {
    throw new Error(`${settingName} must be an http(s) URL without credentials`);
  }
  if (parsed.search || parsed.hash) {
    throw new Error(`${settingName} must not contain a query or hash`);
  }
  validateBackendEndpointPath(rawAbsoluteUrlPath(raw), `${settingName} path`);
  return parsed.toString().replace(/\/+$/, "");
}

function backendUrl(env) {
  const raw = requiredText(env, "QWEN3GUARD_BACKEND_BASE_URL");
  return normalizeBackendBaseUrl(raw, "QWEN3GUARD_BACKEND_BASE_URL");
}

function backendProvider(env) {
  const value = env.QWEN3GUARD_BACKEND_PROVIDER?.trim().toLowerCase() || "qwen";
  if (value !== "qwen" && value !== "minimax") {
    throw new Error("QWEN3GUARD_BACKEND_PROVIDER must be qwen or minimax");
  }
  return value;
}

function miniMaxServiceTier(env) {
  const value = env.QWEN3GUARD_MINIMAX_SERVICE_TIER?.trim().toLowerCase() || "standard";
  if (value !== "standard" && value !== "priority") {
    throw new Error("QWEN3GUARD_MINIMAX_SERVICE_TIER must be standard or priority");
  }
  return value;
}

export function resolveAdapterConfig(env = process.env) {
  const adapterBearerToken = requiredText(env, "QWEN3GUARD_ADAPTER_BEARER_TOKEN");
  const resolvedBackendProvider = backendProvider(env);
  const resolvedBackendUrl = backendUrl(env);
  const backendModel = requiredText(env, "QWEN3GUARD_BACKEND_MODEL");
  const readinessPath = env.QWEN3GUARD_BACKEND_READINESS_PATH?.trim() ||
    (resolvedBackendProvider === "minimax" ? "/v1/models" : "/health");
  validateBackendEndpointPath(readinessPath, "QWEN3GUARD_BACKEND_READINESS_PATH");
  const requestTimeoutMs = integerSetting(
    env,
    "QWEN3GUARD_ADAPTER_REQUEST_TIMEOUT_MS",
    10_000,
    100,
    300_000
  );
  const headersTimeoutMs = integerSetting(
    env,
    "QWEN3GUARD_ADAPTER_HEADERS_TIMEOUT_MS",
    Math.min(5_000, requestTimeoutMs),
    100,
    300_000
  );
  if (headersTimeoutMs > requestTimeoutMs) {
    throw new Error(
      "QWEN3GUARD_ADAPTER_HEADERS_TIMEOUT_MS must not exceed QWEN3GUARD_ADAPTER_REQUEST_TIMEOUT_MS"
    );
  }

  return {
    host: env.QWEN3GUARD_ADAPTER_HOST?.trim() || "127.0.0.1",
    port: integerSetting(env, "QWEN3GUARD_ADAPTER_PORT", 8090, 1, 65_535),
    adapterBearerToken,
    backendProvider: resolvedBackendProvider,
    miniMaxServiceTier: miniMaxServiceTier(env),
    backendBaseUrl: resolvedBackendUrl,
    backendModel,
    backendBearerToken: env.QWEN3GUARD_BACKEND_BEARER_TOKEN?.trim() || undefined,
    backendReadinessPath: readinessPath,
    modelRevision: env.QWEN3GUARD_MODEL_REVISION?.trim() || backendModel,
    maxBodyBytes: integerSetting(
      env,
      "QWEN3GUARD_ADAPTER_MAX_BODY_BYTES",
      262_144,
      1_024,
      16_777_216
    ),
    maxInputChars: integerSetting(
      env,
      "QWEN3GUARD_ADAPTER_MAX_INPUT_CHARS",
      32_768,
      1,
      1_000_000
    ),
    maxConcurrency: integerSetting(
      env,
      "QWEN3GUARD_ADAPTER_MAX_CONCURRENCY",
      2,
      1,
      128
    ),
    maxQueue: integerSetting(env, "QWEN3GUARD_ADAPTER_MAX_QUEUE", 8, 0, 10_000),
    inferenceTimeoutMs: integerSetting(
      env,
      "QWEN3GUARD_ADAPTER_INFERENCE_TIMEOUT_MS",
      15_000,
      100,
      600_000
    ),
    readinessTimeoutMs: integerSetting(
      env,
      "QWEN3GUARD_ADAPTER_READINESS_TIMEOUT_MS",
      2_000,
      100,
      30_000
    ),
    requestTimeoutMs,
    headersTimeoutMs,
    shutdownTimeoutMs: integerSetting(
      env,
      "QWEN3GUARD_ADAPTER_SHUTDOWN_TIMEOUT_MS",
      5_000,
      100,
      60_000
    )
  };
}
