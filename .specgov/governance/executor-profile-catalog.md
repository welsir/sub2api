# Executor Profile Catalog

## Purpose

This catalog enriches the governed execution workflow with reusable executor profiles.

It does not replace:

- `openspec-propose`
- `spec-subagent-orchestrator`
- `delivery-protocol.md`

Instead, it gives the orchestrator a sharper library of execution personas to choose from when defining atomic slices.

## How To Use This Catalog

Use these profiles as routing and prompt-shaping material, not as a second orchestration system.

For each delegated slice:

1. choose the lifecycle slot first
2. choose the narrowest matching executor profile
3. pass only the task-local context needed for that slice
4. keep output contracts explicit and reviewable

Do not dispatch a profile just because it exists. Dispatch only when the current slice benefits from its narrower operating mode.

## Profile Format

Each profile contains:

- `Work type`
- `Slot`
- `Use when`
- `Inputs`
- `Outputs`
- `Avoid when`
- `Notes`

## Scope Profiles

### Code Mapper

**Work type**
- `evidence`

**Slot**
- `scope`

**Use when**
- a task is broad and the master needs a fast map of ownership, key files, and dependency edges
- the master needs to turn a vague implementation area into atomic slices

**Inputs**
- current change goal
- relevant `proposal.md` / `design.md` / `tasks.md`
- target directories or modules

**Outputs**
- likely owning paths
- key files and symbols
- branch points or dependency edges
- unknowns that block clean slicing
- candidate atomic slice boundaries

**Avoid when**
- the task is already cleanly sliced
- the target area is tiny and obvious

**Notes**
- This is one of the highest-value scope profiles because it directly reduces master-side exploration load.

### Search Specialist

**Work type**
- `evidence`

**Slot**
- `scope`

**Use when**
- the master needs fast signal gathering before deeper scoping
- implementation clues are scattered across a large repo
- terminology, feature flags, or entrypoints are unclear

**Inputs**
- search targets
- likely keywords
- target directories if known

**Outputs**
- matched files
- matched symbols or patterns
- candidate entrypoints
- compact relevance summary

**Avoid when**
- the code area is already known
- a deeper semantic map is needed more than a quick signal pass

**Notes**
- Best used before `code-mapper`, not instead of it, when the search space is large.

### Docs Researcher

**Work type**
- `evidence`

**Slot**
- `scope`
- `review`

**Use when**
- the task depends on docs, specs, ADRs, or repo conventions
- the master needs a fact-first summary before routing
- review needs a source-backed statement of intended behavior

**Inputs**
- target docs
- relevant task-definition artifacts
- explicit research question

**Outputs**
- documented facts
- open questions
- inferences clearly separated from facts
- implications for task slicing or review

**Avoid when**
- the task is purely code-local and docs add little value

**Notes**
- Especially useful for keeping “what the docs say” separate from “what we think the code does.”

## Execute Profiles

### Backend Developer

**Work type**
- `production`

**Slot**
- `execute`

**Use when**
- a slice is a backend-focused implementation unit with clear file boundaries
- the work affects services, repositories, controllers, models, or backend tests

**Inputs**
- one atomic slice
- explicit files or module boundary
- acceptance checks
- exact validation commands when available

**Outputs**
- changed files
- concise change summary
- exact validation commands run
- exact validation results
- open risks

**Avoid when**
- the slice spans too many layers
- boundaries are still unresolved
- the task is really a scope or review problem

**Notes**
- Treat this as a narrow backend slice executor, not a broad “backend expert” persona.

### Test Executor

**Work type**
- `evidence`

**Slot**
- `execute`

**Use when**
- the task is to actually run a defined test, command, endpoint, or validation path
- readiness or integration review already identified a concrete runnable check
- the master needs execution evidence rather than another round of static analysis

**Inputs**
- exact test or validation target
- required commands, endpoints, or request examples
- required config or env assumptions
- expected success signal

**Outputs**
- exact commands or requests executed
- exact results
- observed failures or blockers
- missing preconditions if execution could not start
- shortest next step to make the test executable

**Avoid when**
- the task is still deciding what should be tested
- boundaries or prerequisites are still too unclear
- the work is actually a review question, not an execution question

**Notes**
- This profile is for evidence-producing execution, not general test strategy.

## Review Profiles

### Reviewer

**Work type**
- `judgment-support`

**Slot**
- `review`

**Use when**
- a slice needs targeted review for spec alignment or quality risk
- the master wants explicit evidence before accepting a delegated result

**Inputs**
- target slice
- expected behavior from artifact chain
- changed files or diffs
- validation evidence

**Outputs**
- evidence-backed findings
- severity or risk framing
- smallest acceptable fix direction
- residual risk if accepted as-is

**Avoid when**
- no concrete artifact or result exists yet
- review is too broad and should be split

**Notes**
- High-value profile because it makes acceptance less hand-wavy.

### Readiness Reviewer

**Work type**
- `evidence`

**Slot**
- `review`

**Use when**
- the real question is whether something is actually ready to run, test, or hand off
- code, config, tests, and docs all exist, but operational readiness is uncertain
- the master needs an evidence-based answer to “can we directly use this now”

**Inputs**
- target task or workflow
- relevant implementation files
- relevant tests
- config or environment expectations
- task-definition artifacts when available

**Outputs**
- current readiness judgment
- evidence for what is already runnable
- explicit blockers or preconditions
- shortest path to readiness
- residual uncertainty

**Avoid when**
- the task is a normal code review with no readiness question
- the answer depends mostly on new implementation rather than current-state verification

**Notes**
- This profile is about operational truth, not implementation taste.

### Integration Checker

**Work type**
- `evidence`

**Slot**
- `review`
- `integrate`

**Use when**
- the master needs to verify whether multiple pieces really connect into one runnable path
- code and tests exist across modules, but end-to-end linkage is uncertain
- the task asks for the shortest viable path to run or validate a chain

**Inputs**
- integration target
- participating modules or entrypoints
- relevant tests or integration checks
- required config/env assumptions

**Outputs**
- integration path summary
- confirmed working links
- broken or missing links
- exact preconditions to run the chain
- recommended next verification step

**Avoid when**
- the task is still too broad and needs scoping first
- only one isolated module is under review

**Notes**
- Useful when “the code exists” is not the same as “the chain runs.”

## Repair Profiles

### UI Fixer

**Work type**
- `production`

**Slot**
- `repair`

**Use when**
- a UI issue is already localized
- a previous gate found a concrete visual or frontend defect
- the right move is a minimal patch, not a redesign

**Inputs**
- bug description
- affected files
- failure evidence or screenshot context
- acceptance checks

**Outputs**
- minimal patch
- exact affected files
- validation steps
- remaining visual or UX risks

**Avoid when**
- the work is actually a redesign
- the problem is still ambiguous

**Notes**
- The useful pattern here is not “UI” specifically; it is the minimal-patch repair discipline.
