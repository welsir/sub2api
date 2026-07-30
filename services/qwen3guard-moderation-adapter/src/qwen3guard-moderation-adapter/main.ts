/**
 * [INPUT]: Process environment and operating-system termination signals.
 * [OUTPUT]: A listening adapter with structured errors and bounded graceful shutdown.
 * [POS]: TypeScript authoring entrypoint for the Qwen3Guard adapter process.
 *
 * [PROTOCOL]:
 * 1. Update this header when process lifecycle responsibilities change.
 * 2. Update this folder's .folder.md when this file changes.
 */

import { resolveAdapterConfig } from "./config";
import {
  createModerationAdapterServer,
  shutdownModerationAdapterServer,
  type AdapterLogRecord
} from "./server";

const config = resolveAdapterConfig();
const server = createModerationAdapterServer(config);
const logInfo = (record: AdapterLogRecord) => console.info(JSON.stringify(record));
let listening = false;
let shuttingDown = false;

server.once("error", (error: NodeJS.ErrnoException) => {
  console.error(
    JSON.stringify({
      event: listening
        ? "qwen3guard_adapter.server_error"
        : "qwen3guard_adapter.listen_error",
      timestamp: new Date().toISOString(),
      error_code: error.code ?? "unknown"
    })
  );
  process.exitCode = 1;
});

server.listen(config.port, config.host, () => {
  listening = true;
  logInfo({
    event: "qwen3guard_adapter.started",
    timestamp: new Date().toISOString(),
    host: config.host,
    port: config.port,
    model_revision: config.modelRevision
  });
});

async function shutdown(signal: NodeJS.Signals): Promise<void> {
  if (shuttingDown) {
    return;
  }
  shuttingDown = true;
  logInfo({
    event: "qwen3guard_adapter.signal",
    timestamp: new Date().toISOString(),
    signal
  });
  const result = await shutdownModerationAdapterServer(
    server,
    config.shutdownTimeoutMs,
    logInfo
  );
  process.exitCode = result.status === "graceful" ? (process.exitCode ?? 0) : 1;
}

process.once("SIGINT", () => {
  void shutdown("SIGINT");
});
process.once("SIGTERM", () => {
  void shutdown("SIGTERM");
});
