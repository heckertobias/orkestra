# orkestra — Update System (Design)

> **Status: design only.** This document describes a proposed fleet-update subsystem. Nothing here
> is implemented yet. It exists to pin down the model before code is written.

## Goal

Keep a fleet up to date across **three layers**, with the update behaviour configurable **per agent
and per layer** — each can be **manual** (surface "update available", apply on click) or
**automatic** (apply within an optional maintenance window):

1. **orkestra binaries** — the Master and Agent themselves.
2. **Stack images** — the container images of managed Compose stacks.
3. **Host OS** — operating-system package updates (apt/unattended-upgrades, TrueNAS updates).

The Master is the control point; agents connect outbound only (see `docs/02-protocol.md`), so all
update commands ride the existing mTLS stream and all "what's available" data is agent-reported —
the same pattern already used for [federated metrics](./08-deployment.md#federated-agent-metrics).

## What exists today (building blocks)

- The Master already records each agent's binary version: `servers.agent_version`, set from the
  `Hello` message (`internal/master/agentgw/handler.go`).
- Image freshness for stacks is partly handled by the Compose `pull_policy` in the converge engine
  (`ensureImage` / `shouldPull`, `internal/agent/compose/converge.go`). That covers *pull on
  deploy*, not *detect a newer image and redeploy*.
- Agent mTLS certs auto-renew (`internal/agent/conn/conn.go`) — a precedent for autonomous,
  policy-gated agent actions, but unrelated to binary/image/OS updates.

## Policy model

One row per **(agent, layer)**, plus fleet-wide defaults:

```
update_policies
  server_id     uuid        -- FK servers.id (NULL row = fleet default)
  layer         text        -- 'orkestra' | 'images' | 'os'
  mode          text        -- 'manual' | 'automatic'
  window_cron   text NULL   -- optional maintenance window for automatic mode
  auto_reboot   bool        -- os layer only: allow reboots
  updated_at    bigint
  PRIMARY KEY (server_id, layer)
```

Resolution: agent-specific row wins over the fleet default. UI lets you set each layer to
Manual/Automatic independently per agent (matches the requirement: *"je Agent manuell oder
automatisch und je Typ"*).

## Reported availability

Agents report what updates are available so the Master can show badges / drive automation:

```
available_updates
  server_id     uuid
  layer         text
  current       text        -- current version / digest / package count
  candidate     text        -- target version / digest / "N packages"
  detail        jsonb       -- e.g. per-image digests, changelog URL, security-only flag
  detected_at   bigint
  PRIMARY KEY (server_id, layer)
```

How each layer detects a candidate:

| Layer | Detection (agent side) |
|---|---|
| orkestra | Master compares `servers.agent_version` against its own build/target version; Master version is known locally. |
| images | Agent resolves the remote digest for each managed image (registry `HEAD`/manifest) and compares to the running digest. Extends `ensureImage`. |
| os | Agent queries the package manager (`apt-get -s upgrade` / TrueNAS update API) for pending updates, flagging security-only. |

## Applying an update

New RPCs on the Master→Agent stream (`proto/orkestra/v1/agent.proto`), correlated by `request_id`
like the existing request/response messages:

```proto
message UpdateRequest {
  string layer = 1;          // 'orkestra' | 'images' | 'os'
  string target = 2;         // optional pin; empty = latest candidate
  bool   allow_reboot = 3;   // os layer
}
message UpdateResult {
  bool   success = 1;
  string from = 2;
  string to = 3;
  bool   reboot_required = 4;
  string error = 5;
}
```

Per-layer apply behaviour:

- **orkestra (agent binary):**
  - *Container agent (TrueNAS/Docker):* "update" = pull the new image tag and recreate the
    container. Either via the platform's own updater (TrueNAS app "Update" button, Watchtower) or
    orkestra-triggered — the latter touches the self-management edge (an agent recreating its own
    container). **Open question** below.
  - *systemd agent:* an updater downloads the new signed binary from GHCR/Releases, verifies the
    checksum/signature, swaps `/usr/local/bin/orkestra-agent`, and restarts the unit.
- **orkestra (Master):** updated last, out of band (Compose/systemd), never by an agent.
- **images:** re-resolve digests and redeploy affected stacks through the normal converge path.
- **os:** run the package manager; if `reboot_required` and `auto_reboot`, drain and reboot within
  the window; otherwise report `reboot_required` and wait for a manual trigger.

## UI

Per-agent settings page, one control per layer: **Manual / Automatic** + optional window (and, for
OS, an auto-reboot toggle). A fleet "Updates" view lists agents with `available_updates` and an
**Apply** button for manual layers; automatic layers apply within their window and show history.

## Rollout safety

- **Rolling, never fleet-wide-at-once:** cap concurrent updates; honour per-agent windows.
- **Health-gate each step:** after applying, wait for the agent to reconnect / report healthy
  before proceeding to the next; on failure, stop and surface the error.
- **Master last:** never update the Master in the same pass as its agents.
- **Audit + events:** every applied/failed update writes to `audit_log` and `events`.

## Open questions

1. **Self-management of a container agent's own binary.** An agent recreating the container it runs
   in is inherently fragile. Options: (a) delegate to the platform updater (TrueNAS button /
   Watchtower) and only *surface* availability in orkestra; (b) a tiny sidecar/one-shot that
   recreates the agent; (c) orkestra never auto-updates container agents, only systemd ones.
   Leaning toward (a) for TrueNAS.
2. **Binary signing / provenance** for the systemd self-updater (cosign vs checksum-only).
3. **OS updates on TrueNAS** go through the TrueNAS update API, not apt — likely a separate agent
   capability flag rather than a generic "os" implementation.
4. **Co-located Master+Agent** (`docs/08-deployment.md`): fleet OS/binary updates must special-case
   the host that also runs the Master (drain/skip to avoid killing the control plane mid-update).
