## ADDED Requirements

### Requirement: Private-only adapter reachability
The moderation adapter SHALL be reachable from the approved Sub2API host through a private Tailscale path and SHALL NOT require a publicly exposed Windows, WSL2, model-server, or adapter port.

#### Scenario: Approved Sub2API host connects
- **WHEN** the running Sub2API environment calls the adapter through its approved tailnet identity and valid Bearer secret
- **THEN** the request reaches the adapter and no public ingress path is involved

#### Scenario: Unapproved tailnet identity connects
- **WHEN** another tailnet identity attempts to reach the adapter port
- **THEN** the Tailscale ACL denies the connection before application authentication

### Requirement: Runtime-boundary connectivity proof
Connectivity acceptance MUST originate from the same container or network boundary that performs production Sub2API moderation calls.

#### Scenario: Host-only success is insufficient
- **WHEN** the cloud host can reach `/readyz` but the Sub2API container cannot complete an authenticated moderation request
- **THEN** private connectivity remains unaccepted and rollout does not proceed

### Requirement: Defense in depth
The adapter MUST require a dedicated Bearer secret even on the private tailnet, and the model backend MUST remain loopback-only or otherwise inaccessible to unapproved peers.

#### Scenario: Tailnet member lacks adapter secret
- **WHEN** an allowed network peer reaches the adapter without the valid Bearer secret
- **THEN** the adapter rejects the request and does not forward it to Qwen3Guard

### Requirement: Restart and recovery behavior
The Windows/WSL2 deployment SHALL document and verify service startup and recovery after Windows reboot, WSL restart, broadband reconnect, tailnet reconnect, and model-process failure. Windows sleep or hibernation SHALL be disabled during the declared service window.

#### Scenario: Windows reboot
- **WHEN** the Windows host reboots and returns to the declared service window
- **THEN** Tailscale, the model backend, and the adapter return to ready state without requiring an undocumented manual command

#### Scenario: Tailnet reconnect
- **WHEN** the broadband or Tailscale session is interrupted and restored
- **THEN** the Sub2API runtime can complete a new authenticated moderation probe and the recovery is visible in operational evidence

### Requirement: Secret and endpoint hygiene
Repository artifacts SHALL use placeholders or environment references for adapter secrets and tailnet-specific addresses; real secrets SHALL NOT be committed or printed by validation scripts.

#### Scenario: Configuration is checked into version control
- **WHEN** deployment examples or smoke scripts are added
- **THEN** they reference external secret inputs and redact authentication material from command output
