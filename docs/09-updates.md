# orkestra — Fleet Updates

> **Status.** This chapter describes the update model as it is designed and as far as it is built.
> Today the **data model and the reporting path into the Master exist**: the schema
> (`update_policies`, `available_updates`), the queries that resolve a policy and upsert
> availability, and the Master handler that persists `StatusReport.available_updates`. Agent-side
> detection, an API/UI for policies, and actually applying an update are **not implemented**.

Keeping a fleet of Docker hosts current is three different problems wearing one coat, so orkestra
splits them into **layers** and lets each layer be governed separately.

## Layers

| Layer | What it updates | Restart semantics |
|---|---|---|
| `orkestra` | The orkestra binaries themselves (agent, and the master where co-located) | Service restart; the agent must survive replacing itself |
| `images` | Container images referenced by the stacks running on a host | Per-stack recreate, following the normal converge path |
| `os` | Operating-system packages of the host | May require a reboot |

The three have genuinely different risk profiles — pulling a new image is a routine deploy, while
an OS update may take the host down — which is why one global "auto-update" switch would be the
wrong abstraction.

## Policy model

A policy answers: *for this host and this layer, who decides and when?*

```sql
update_policies (server_id, layer, mode, window_cron, auto_reboot, updated_at)
```

- `server_id IS NULL` → the **fleet default** for that layer (one row per layer).
- `server_id` set → the **per-agent override** for that layer (one row per server and layer).
- `mode` is `manual` (an operator triggers it) or `automatic`.
- `window_cron` optionally restricts automatic runs to a maintenance window.
- `auto_reboot` applies to the `os` layer: may the host reboot on its own when the update needs it?

Resolution is "most specific wins": the query looks for a row matching `(server_id, layer)` and
falls back to the fleet row for that layer. A fleet default plus a handful of exceptions is the
expected shape — for example `images: automatic` everywhere, but `manual` on the host that also
runs the Master.

## Reported availability

The Agent reports what it has found in its regular heartbeat, so no extra channel is needed:

```protobuf
message StatusReport {
  repeated StackStatus     stacks            = 1;
  int64                    reported_at_ms    = 2;
  repeated AvailableUpdate available_updates = 3;
}

message AvailableUpdate {
  string layer     = 1;  // 'orkestra' | 'images' | 'os'
  string current   = 2;  // current version / digest / package count
  string candidate = 3;  // target version / digest / "N packages"
}
```

The Master upserts each entry into `available_updates`, keyed by `(server_id, layer)`, so the table
always holds the latest finding per host and layer rather than a growing history. Rows cascade away
with the server.

Availability is deliberately **decoupled from application**: detecting that something is available
is cheap, safe, and useful on its own (the fleet view can show "3 hosts have OS updates pending")
even where the policy says a human decides.

## What is missing

To close the loop, three pieces are still needed:

1. **Detection on the agent** — compare its own version against the release channel, resolve image
   tags to digests, query the OS package manager, and fill `available_updates` in the heartbeat.
2. **Policy management** — RPCs and a UI to read and write fleet defaults and per-agent overrides.
3. **Application** — a command path that performs the update for a layer, respecting the window and
   reboot flags, and reports the result. The `orkestra` layer is the delicate one: an agent that
   replaces its own binary has to hand over cleanly, and a co-located Master must not be recreated
   by the agent it manages (see [08-deployment.md](08-deployment.md) § "Co-locating the Master and
   an Agent on one host").
