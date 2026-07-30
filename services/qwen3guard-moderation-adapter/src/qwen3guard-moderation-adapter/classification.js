/**
 * [INPUT]: Structured Qwen3Guard Safe, Controversial, or Unsafe output.
 * [OUTPUT]: Versioned labels, normalized mapping metadata, and deterministic policy scores.
 * [POS]: Checked-in JavaScript taxonomy and parser boundary for the adapter runtime.
 *
 * [PROTOCOL]:
 * 1. Update this header when parsing or mapping responsibilities change.
 * 2. Update this folder's .folder.md when this file changes.
 */

export const MAPPING_REVISION = "qwen3guard-openai-policy-v1";

export const evaluatedCategories = [
  "harassment",
  "harassment/threatening",
  "hate",
  "hate/threatening",
  "illicit",
  "illicit/violent",
  "self-harm",
  "self-harm/intent",
  "self-harm/instructions",
  "sexual",
  "sexual/minors",
  "violence",
  "violence/graphic"
];

function cleanLines(value) {
  return value
    .replace(/```(?:json|text)?/gi, "")
    .replace(/```/g, "")
    .replace(/<\|im_end\|>/g, "")
    .split(/\r?\n/)
    .map((line) => line.trim())
    .filter(Boolean);
}

function labelFrom(value) {
  if (value === "Safe" || value === "Controversial" || value === "Unsafe") {
    return value;
  }
  throw new Error("unsupported Qwen3Guard safety label");
}

function categoryList(value) {
  if (value === undefined || value === null || value === "") {
    return [];
  }
  if (typeof value === "string") {
    return value
      .split(/[,;|]/)
      .map((category) => category.trim())
      .filter(Boolean);
  }
  if (Array.isArray(value) && value.every((category) => typeof category === "string")) {
    return value.map((category) => category.trim()).filter(Boolean);
  }
  throw new Error("invalid Qwen3Guard categories");
}

function parseJsonOutput(output) {
  if (!output.startsWith("{")) {
    return undefined;
  }
  let parsed;
  try {
    parsed = JSON.parse(output);
  } catch {
    throw new Error("malformed Qwen3Guard JSON output");
  }
  if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
    throw new Error("invalid Qwen3Guard JSON output");
  }
  return {
    label: labelFrom(parsed.safety ?? parsed.label),
    categories: categoryList(parsed.categories ?? parsed.category)
  };
}

function parseTextOutput(output) {
  const finalMarker = "<|im_start|>assistant/final";
  const finalOutput = output.includes(finalMarker)
    ? output.slice(output.lastIndexOf(finalMarker) + finalMarker.length)
    : output;
  const lines = cleanLines(finalOutput);
  if (lines.length === 0) {
    throw new Error("empty Qwen3Guard output");
  }

  const labeled = lines[0].match(/^(?:safety|label)\s*:\s*(.+)$/i);
  const label = labelFrom(labeled ? labeled[1] : lines[0]);
  const categoryLine = lines.find((line) => /^(?:categories|category)\s*:/i.test(line));
  const categories = categoryLine
    ? categoryList(categoryLine.replace(/^(?:categories|category)\s*:\s*/i, ""))
    : labeled
      ? []
      : categoryList(lines.slice(1).join(","));

  return { label, categories };
}

function normalizedCategory(category) {
  return category.toLowerCase().replace(/[_–—]/g, "-").replace(/\s+/g, " ").trim();
}

function mappedCategories(rawCategories) {
  const mapped = new Set();
  let unknownCount = 0;
  for (const rawCategory of rawCategories) {
    const category = normalizedCategory(rawCategory);
    let recognized = false;
    if (category.includes("violent") && !category.includes("non-violent")) {
      mapped.add("violence");
      mapped.add("illicit/violent");
      recognized = true;
    }
    if (!recognized && category.includes("sexual")) {
      mapped.add("sexual");
      recognized = true;
    }
    if (!recognized && (category.includes("suicide") || category.includes("self-harm"))) {
      mapped.add("self-harm");
      recognized = true;
    }
    if (
      !recognized &&
      (category.includes("illegal") ||
        category.includes("jailbreak") ||
        category.includes("unethical") ||
        category.includes("personally identifiable") ||
        category === "pii" ||
        category.includes("privacy") ||
        category.includes("copyright"))
    ) {
      mapped.add("illicit");
      recognized = true;
    }
    if (!recognized) {
      unknownCount += 1;
    }
  }
  return { mapped, unknownCount };
}

export function parseAndMapClassification(output) {
  const trimmed = output.trim();
  if (!trimmed) {
    throw new Error("empty Qwen3Guard output");
  }
  const parsed = parseJsonOutput(trimmed) ?? parseTextOutput(trimmed);
  const policyValue = parsed.label === "Safe" ? 0 : parsed.label === "Controversial" ? 0.5 : 1;
  const categoryResult = mappedCategories(parsed.categories);
  const hadMappedCategory = categoryResult.mapped.size > 0;
  if (
    parsed.label !== "Safe" &&
    (parsed.categories.length === 0 || categoryResult.unknownCount > 0)
  ) {
    categoryResult.mapped.add("illicit");
  }
  const mappedCategoryList =
    parsed.label === "Safe"
      ? []
      : evaluatedCategories.filter((category) => categoryResult.mapped.has(category));
  const categoryMapping =
    parsed.label === "Safe"
      ? "none"
      : categoryResult.unknownCount > 0
        ? hadMappedCategory
          ? "mapped_with_fallback"
          : "fallback"
        : parsed.categories.length === 0
          ? "fallback"
          : "mapped";

  const categoryScores = Object.fromEntries(
    evaluatedCategories.map((category) => [
      category,
      parsed.label !== "Safe" && categoryResult.mapped.has(category) ? policyValue : 0
    ])
  );
  const categories = Object.fromEntries(
    evaluatedCategories.map((category) => [category, categoryScores[category] > 0])
  );

  return {
    label: parsed.label,
    sourceCategoryCount: parsed.categories.length,
    mappedCategories: mappedCategoryList,
    categoryMapping,
    flagged: parsed.label === "Unsafe",
    policyValue,
    categories,
    categoryScores,
    mappingRevision: MAPPING_REVISION
  };
}
