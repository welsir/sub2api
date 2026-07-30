/**
 * [INPUT]: Checked-in JavaScript Qwen3Guard classification runtime.
 * [OUTPUT]: Runtime-path parity assertions for critical unsafe fallback behavior.
 * [POS]: JavaScript mirror smoke coverage for the standalone moderation adapter.
 *
 * [PROTOCOL]:
 * 1. Update this header when test responsibilities change.
 * 2. Update this folder's .folder.md when this file changes.
 */

import { describe, expect, it } from "vitest";

import {
  MAPPING_REVISION,
  parseAndMapClassification
} from "../../src/qwen3guard-moderation-adapter/classification.js";

describe("Qwen3Guard JavaScript runtime classification", () => {
  it("keeps the checked-in runtime mapping revision and unsafe fallback aligned", () => {
    const result = parseAndMapClassification("Safety: Unsafe\nCategories: Unknown Future Risk");

    expect(result.mappingRevision).toBe(MAPPING_REVISION);
    expect(result.flagged).toBe(true);
    expect(result.categoryScores.illicit).toBe(1);
  });

  it("does not turn malformed runtime output into Safe", () => {
    expect(() => parseAndMapClassification("Safety: Unknown")).toThrow();
  });
});
