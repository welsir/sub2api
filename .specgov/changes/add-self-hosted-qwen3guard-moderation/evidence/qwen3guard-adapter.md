# Qwen3Guard Moderation Adapter Evidence

Date: 2026-07-30

## Boundary

- Repository: `/Users/welsir/.config/superpowers/worktrees/sub2api/omni-latest-selected-customs`
- Product branch: `dev/omni`
- Owned package: `services/qwen3guard-moderation-adapter/`
- Sub2API ownership commit: `79b046f82aaeae392baa0ad79df9456f7a0a4839`
- The migrated runtime and tests match the independently reviewed adapter content from `de23711` / `1e2684f`; the mistaken TML placement was removed by TML commit `a74b1b2`.
- No push, pull, rebase, production change, or cross-branch synchronization was performed.

## Accepted Behavior

- The repository owns a separate OpenAI-shaped adapter with authenticated `POST /v1/moderations`, `/healthz`, and `/readyz`.
- Supported text inputs map deterministic Qwen `Safe`, `Controversial`, and `Unsafe` output into Sub2API categories and fixed policy scores.
- Unknown or missing unsafe categories use the unsafe `illicit` fallback; malformed or incomplete model output is never converted into `Safe`.
- Input size, concurrency, queueing, inference, streaming, and shutdown paths are bounded.
- Backend URL origin/path containment prevents Bearer credentials from crossing to another origin and rejects ambiguous backslash or dot-segment targets.
- Operational logs and metrics retain correlation, revision, size/hash, classification, latency, overload, timeout, cancellation, and parse-error metadata without prompts or secrets.
- Sub2API request-routing behavior remains unchanged.

## Review Gates

- Specification review: approved.
- Quality review: approved after two repair rounds.
- Repairs covered request-target parsing, stream bounds, log redaction, finish reasons, backend URL containment, timeout/cancellation classification, shutdown deadlines, and cross-origin Bearer protection.

## Integrated Verification

Passed from the Sub2API-owned package:

- Source and tests, excluding the ownership description, are byte-equivalent to the reviewed adapter content.
- `pnpm test`: 5 files, 72/72 tests.
- `pnpm typecheck`.
- `node --check` for all 9 added checked-in JavaScript files.
- `git diff --cached --check` before the Sub2API ownership commit.
- Scoped high-confidence secret and public-IPv4 scan: `secret_hits=0 public_ipv4_hits=0`.

This historical checkpoint used checked-in JavaScript mirrors. The later
MiniMax-first candidate in `evidence/minimax-local-candidate.md` supersedes that
build boundary: TypeScript is now the only source and Docker runs compiled
`dist/` output verified by the same package test suite.

## Corrected Ownership

The first implementation was incorrectly committed to `tml-omni-gateway`.
No user instruction established that ownership, and Gateway is not in the
moderation request path. The accepted runtime was migrated without behavioral
changes into Sub2API `dev/omni`; TML commit `a74b1b2` removes the misplaced
runtime, tests, package scripts, environment example, and README section.

## Remaining Boundary

This evidence proves the local adapter contract and its ownership inside Sub2API `dev/omni`. It does not prove Windows/WSL2 GPU compatibility, a real Qwen model revision, Tailscale reachability, production traffic quality, the exact production `tml` ID, observation mode, or blocking enforcement.
