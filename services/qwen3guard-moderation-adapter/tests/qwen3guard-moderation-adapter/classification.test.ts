/**
 * [INPUT]: Qwen3Guard structured classifier output fixtures.
 * [OUTPUT]: Contract assertions for parser and deterministic moderation mapping.
 * [POS]: TypeScript source-path unit coverage for the standalone moderation adapter.
 *
 * [PROTOCOL]:
 * 1. Update this header when test responsibilities change.
 * 2. Update this folder's .folder.md when this file changes.
 */

import { describe, expect, it } from "vitest";

import {
  MAPPING_REVISION,
  evaluatedCategories,
  parseAndMapClassification
} from "../../src/qwen3guard-moderation-adapter/classification";

describe("Qwen3Guard classification mapping", () => {
  it("maps Safe to zero policy scores", () => {
    const result = parseAndMapClassification("Safety: Safe");

    expect(result.label).toBe("Safe");
    expect(result.flagged).toBe(false);
    expect(result.policyValue).toBe(0);
    expect(result.mappingRevision).toBe(MAPPING_REVISION);
    expect(Object.values(result.categoryScores).every((score) => score === 0)).toBe(true);
  });

  it("maps Controversial to an observable intermediate policy value without flagging", () => {
    const result = parseAndMapClassification(
      "Safety: Controversial\nCategories: Non-violent Illegal Acts"
    );

    expect(result.label).toBe("Controversial");
    expect(result.flagged).toBe(false);
    expect(result.policyValue).toBe(0.5);
    expect(result.categoryScores.illicit).toBe(0.5);
  });

  it("maps violent Unsafe output to both evaluated violent categories", () => {
    const result = parseAndMapClassification(
      "<|im_start|>assistant/final\nUnsafe\nViolent"
    );

    expect(result.flagged).toBe(true);
    expect(result.policyValue).toBe(1);
    expect(result.categoryScores.violence).toBe(1);
    expect(result.categoryScores["illicit/violent"]).toBe(1);
  });

  it.each([
    ["Sexual", "sexual"],
    ["Suicide & Self-Harm", "self-harm"],
    ["Jailbreak", "illicit"],
    ["Personally Identifiable Information", "illicit"],
    ["Copyright Violations", "illicit"]
  ] as const)("maps %s Unsafe output to %s", (rawCategory, expectedCategory) => {
    const result = parseAndMapClassification(
      JSON.stringify({ safety: "Unsafe", categories: [rawCategory] })
    );

    expect(result.categoryScores[expectedCategory]).toBe(1);
  });

  it("falls back unknown or absent Unsafe categories to illicit", () => {
    const unknown = parseAndMapClassification("Safety: Unsafe\nCategories: Emerging Risk");
    const absent = parseAndMapClassification("Safety: Unsafe");

    expect(unknown.categoryScores.illicit).toBe(1);
    expect(absent.categoryScores.illicit).toBe(1);
  });

  it.each(["", "Safety: Maybe", "not structured", "{\"safety\":\"Safe\""])(
    "rejects empty, unknown, malformed, or truncated output: %j",
    (output) => {
      expect(() => parseAndMapClassification(output)).toThrow();
    }
  );

  it("includes the categories Sub2API evaluates in every score record", () => {
    const result = parseAndMapClassification("Safety: Safe");

    expect(Object.keys(result.categoryScores)).toEqual(evaluatedCategories);
  });
});
