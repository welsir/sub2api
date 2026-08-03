# Delivery Protocol

## Purpose

Use this protocol to run coding work after OpenSpec change artifacts exist. Keep change definition and delivery governance separate.

## Artifact Chain

Before execution, confirm the current state of:

1. `proposal.md`
2. `design.md`
3. `tasks.md`

If an earlier artifact is missing or unstable, repair that first. Do not treat governance files as a substitute for task-definition files.

## Master Responsibilities

The master should focus on:

- reading the artifact chain
- deciding the next execution mode
- defining atomic slices
- dispatching subagents
- accepting or rejecting results
- integrating approved results
- rerunning final critical checks

The master should avoid becoming the default implementation worker once the task is sliceable.

## Work Classification

Classify the next unit of work before routing it:

### Evidence work

Work whose primary output is evidence, confirmation, or operational truth.

Examples:

- readiness checks
- integration verification
- test execution
- environment and config checks
- code mapping

Default routing bias:

- delegate if the evidence can be produced as an atomic slice

### Production work

Work whose primary output is a code or configuration change.

Default routing bias:

- delegate after slice boundaries and acceptance checks are clear

### Judgment work

Work whose primary output is a routing, acceptance, or synthesis decision.

Default routing bias:

- keep with the master unless it can be split into delegated evidence work plus later synthesis

## Entry Routing

Choose the next route in this order:

1. artifact repair
2. state reconciliation
3. planning or scoping
4. evidence slice
5. production slice
6. integration
7. verification
8. local exception

Do not jump to executor work if the real next need is planning, evidence, integration, or verification.

## Default Execution Policy

After `tasks.md` exists, assume subagents should perform most execution work unless there is a strong reason not to.

Strong reasons to stay local:

- the next action takes only a few minutes
- the next action exists only to lock one unresolved boundary
- delegation would cost more than the work itself

Evidence work should not default to the master merely because it involves testing or verification. If the evidence can be gathered as a bounded slice, delegation should be preferred.

## Atomic Slice Checklist

Dispatch only when the slice has:

- one goal
- one primary deliverable
- explicit write boundaries
- explicit acceptance checks
- no hidden dependency on an unresolved design question

If these are missing, run a scoping pass first.

## Lifecycle Slots

Use these slots to structure delivery rounds:

- `scope`
- `execute`
- `review`
- `repair`
- `integrate`

These are workflow slots, not a fixed cast of agents. Instantiate only what the task needs.

## Role Contracts

Keep lanes clean:

- master chooses routes, dispatches work, and owns final acceptance
- planner or scoper defines slices and boundaries
- executor performs bounded production work
- verifier gathers and interprets evidence
- integrator reconciles accepted slices and reruns shared checks

Returned work is never the same thing as accepted work.

## Dispatch Rules

### Scope

Use a scoping pass when:

- the current task is still too broad
- ownership is unclear
- boundaries overlap too much
- the master is accumulating too much local analysis

Expected output:

- slice definitions
- boundaries
- acceptance checks
- open risks

### Execute

Use execution slices for concrete deliverables such as:

- one module change
- one controller/service/repository change
- one test addition
- one documentation patch

Expected output:

- changed files
- key change summary
- exact validation commands
- exact validation results
- open risks

### Review

Use review passes to check either:

- spec alignment
- quality and regression risk

Do not merge review types into one vague pass when targeted review is cheaper.

### Repair

Use repair passes only after a concrete failed gate.

Expected output:

- targeted fix
- rerun of the relevant checks
- updated risk note if needed

### Integrate

Use integration when multiple accepted slices must be combined, reconciled, or verified together.

## Verification Gates

No slice is complete until these gates are satisfied as applicable:

1. `spec gate`
   Output matches the artifact chain and requested slice.
2. `quality gate`
   Output is maintainable and does not hide obvious regression risk.
3. `command gate`
   Important validation commands are rerun locally by the master.

## Verification Sequence

For any acceptance decision:

1. identify the claim
2. identify the evidence needed
3. run the checks
4. read the outputs
5. compare outputs against the slice or gate
6. accept, reject, or return for repair
7. advance ledgers or logs only after the outcome is known

## Run Logging

After a real task, record a short run log using the template.

The purpose is not to compute fake budget numbers. The purpose is to capture:

- what was delegated
- what stayed local
- where waste appeared
- whether the chosen pattern felt worth repeating

Run logs are observational inputs only.

Do not automatically promote run-log conclusions into protocol changes.

Changes to this protocol require explicit human review and approval.
