/**
 * [INPUT]: Adapter request outcomes, classifications, latency, and scheduler state.
 * [OUTPUT]: Bounded in-memory counters and Prometheus text exposition.
 * [POS]: Checked-in JavaScript metrics surface for the standalone adapter.
 *
 * [PROTOCOL]:
 * 1. Update this header when metric responsibilities change.
 * 2. Update this folder's .folder.md when this file changes.
 */

export class AdapterMetrics {
  constructor() {
    this.outcomes = new Map();
    this.classifications = new Map();
    this.latencyCount = 0;
    this.latencyTotalMs = 0;
    this.inFlight = 0;
    this.queueDepth = 0;
  }

  incrementRequest(outcome) {
    this.outcomes.set(outcome, (this.outcomes.get(outcome) ?? 0) + 1);
  }

  incrementClassification(label) {
    this.classifications.set(label, (this.classifications.get(label) ?? 0) + 1);
  }

  observeLatency(milliseconds) {
    this.latencyCount += 1;
    this.latencyTotalMs += Math.max(0, milliseconds);
  }

  setScheduler(inFlight, queueDepth) {
    this.inFlight = Math.max(0, inFlight);
    this.queueDepth = Math.max(0, queueDepth);
  }

  render() {
    const lines = [
      "# HELP qwen3guard_adapter_requests_total Moderation adapter requests by outcome.",
      "# TYPE qwen3guard_adapter_requests_total counter"
    ];
    for (const [outcome, value] of [...this.outcomes.entries()].sort()) {
      lines.push(`qwen3guard_adapter_requests_total{outcome="${outcome}"} ${value}`);
    }
    lines.push(
      "# HELP qwen3guard_adapter_classifications_total Parsed classifications by label.",
      "# TYPE qwen3guard_adapter_classifications_total counter"
    );
    for (const [label, value] of [...this.classifications.entries()].sort()) {
      lines.push(`qwen3guard_adapter_classifications_total{label="${label}"} ${value}`);
    }
    lines.push(
      "# TYPE qwen3guard_adapter_latency_ms summary",
      `qwen3guard_adapter_latency_ms_count ${this.latencyCount}`,
      `qwen3guard_adapter_latency_ms_sum ${this.latencyTotalMs}`,
      "# TYPE qwen3guard_adapter_in_flight gauge",
      `qwen3guard_adapter_in_flight ${this.inFlight}`,
      "# TYPE qwen3guard_adapter_queue_depth gauge",
      `qwen3guard_adapter_queue_depth ${this.queueDepth}`
    );
    return `${lines.join("\n")}\n`;
  }
}
