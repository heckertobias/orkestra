# orkestra — Communication Protocol (gRPC / ConnectRPC)

## Overview

orkestra uses **ConnectRPC** (`connectrpc.com/connect`) as its RPC framework. ConnectRPC is
protobuf-first and natively speaks three protocols over HTTP/2:

- **gRPC** (binary protobuf framing) — used by Agent↔Master bidirectional streams.
- **Connect protocol** (JSON or binary) — used by the browser SPA (no gRPC-web proxy needed).

One protobuf schema, one Go server, both clients happy.

---

## Connection Lifecycle

### 1. Enrollment (one-time)

Before an Agent can connect, it must be enrolled. This happens once per server.

```
Agent                                          Master
  │                                              │
  │── EnrollRequest ────────────────────────────▶│
  │   {bootstrap_token, csr (PEM), node_info}    │
  │                                              │  validate token
  │                                              │  sign CSR with internal CA
  │                                              │  persist server record
  │◀── EnrollResponse ───────────────────────────│
  │    {client_cert (PEM), ca_bundle, agent_id}  │
  │                                              │
  │  Agent persists cert+key to                  │
  │  /etc/orkestra/agent/{cert,key}.pem         │
```

The `bootstrap_token` is a short-lived, use-limited token created by an operator in the UI.
The Agent generates its own RSA/ECDSA keypair and CSR locally; the private key never leaves the
server.

### 2. Connect (long-lived bidirectional stream)

After enrollment, the Agent opens a persistent bidi-stream authenticated with mTLS:

```
Agent                                          Master
  │── TLS ClientHello (client cert = agent cert) ▶│
  │◀── TLS ServerHello (server cert) ─────────────│
  │  [mTLS handshake — Master verifies agent cert  │
  │   against internal CA; checks revocation list] │
  │                                                │
  │── AgentMessage{Hello} ───────────────────────▶│
  │   {agent_id, version, docker_version,          │
  │    host_info, os, arch}                        │
  │                                                │
  │  Master registers session:                     │
  │  agentID → stream handle                       │
  │                                                │
  │◀── MasterMessage{ApplyDesiredState} ───────────│  (current desired state)
  │                                                │
  │  [stream stays open indefinitely]              │
  │                                                │
  │── AgentMessage{StatusReport} ───────────────▶│  (periodic heartbeat, ~30s)
  │◀── MasterMessage{Ping} ────────────────────────│  (keepalive)
  │── AgentMessage{Pong} ───────────────────────▶│
```

### 3. Reconnect

On any stream error or disconnect:
- Agent reconnects with **exponential backoff + jitter** (base 1 s, max 60 s).
- Master marks Agents as `offline` after missing 3 consecutive heartbeat windows.
- On reconnect, Master immediately pushes the current Desired State again.

---

## Protobuf Definitions

### `agent.proto` — Agent↔Master stream

```protobuf
syntax = "proto3";
package orkestra.v1;

service AgentService {
  // One-time enrollment; no mTLS required (server TLS only), token proves identity.
  rpc Enroll(EnrollRequest) returns (EnrollResponse);

  // Persistent bidirectional stream (requires mTLS).
  rpc Connect(stream AgentMessage) returns (stream MasterMessage);

  // Agent renews its cert before expiry (requires mTLS with current cert).
  rpc RenewCert(RenewCertRequest) returns (RenewCertResponse);
}

// ─── Master → Agent ───────────────────────────────────────────────────────────

message MasterMessage {
  string request_id = 15;  // correlation ID for async responses
  oneof payload {
    ApplyDesiredState apply_desired_state = 1;
    ExecCommand       exec_command        = 2;
    LogRequest        log_request         = 3;
    StatsRequest      stats_request       = 4;
    CancelStream      cancel_stream       = 5;  // stop a log/stats stream
    Ping              ping                = 6;
  }
}

message ApplyDesiredState {
  repeated StackDesiredState stacks = 1;  // full desired state for this server
}

message StackDesiredState {
  string   stack_id    = 1;
  string   version     = 2;
  string   compose_yaml = 3;
  map<string, string> env_vars = 4;
  repeated ResolvedSecret secrets = 5;  // pre-resolved by Master
  DesiredStatus status = 6;             // RUNNING | STOPPED | REMOVED
}

message ResolvedSecret {
  string name        = 1;  // binding name used in compose
  bytes  value       = 2;  // plaintext value (only in flight over mTLS)
  SecretTarget target = 3; // ENV | FILE | DOCKER_SECRET
  string env_key     = 4;  // if ENV
  string file_path   = 5;  // if FILE (mounted into container)
}

enum DesiredStatus {
  DESIRED_STATUS_UNSPECIFIED = 0;
  DESIRED_STATUS_RUNNING     = 1;
  DESIRED_STATUS_STOPPED     = 2;
  DESIRED_STATUS_REMOVED     = 3;
}

message ExecCommand {
  string container_id = 1;
  CommandType type    = 2;
  repeated string args = 3;  // for EXEC
}

enum CommandType {
  COMMAND_TYPE_UNSPECIFIED = 0;
  COMMAND_TYPE_START       = 1;
  COMMAND_TYPE_STOP        = 2;
  COMMAND_TYPE_RESTART     = 3;
  COMMAND_TYPE_PULL        = 4;
  COMMAND_TYPE_REMOVE      = 5;
  COMMAND_TYPE_EXEC        = 6;
  COMMAND_TYPE_PRUNE       = 7;
}

message LogRequest {
  string container_id = 1;
  bool   follow       = 2;
  string since        = 3;  // RFC3339 or duration e.g. "5m"
  int32  tail         = 4;  // 0 = all
  bool   timestamps   = 5;
}

message StatsRequest {
  repeated string container_ids = 1;  // empty = all managed containers
}

message CancelStream { string stream_id = 1; }
message Ping         { int64 timestamp_ms = 1; }

// ─── Agent → Master ───────────────────────────────────────────────────────────

message AgentMessage {
  string request_id = 15;
  oneof payload {
    Hello         hello          = 1;
    StatusReport  status_report  = 2;
    LogChunk      log_chunk      = 3;
    StatsChunk    stats_chunk    = 4;
    CommandResult command_result = 5;
    DockerEvent   docker_event   = 6;
    Pong          pong           = 7;
  }
}

message Hello {
  string agent_id      = 1;
  string agent_version = 2;
  string docker_version = 3;
  string hostname      = 4;
  string os            = 5;
  string arch          = 6;
  int32  cpu_count     = 7;
  int64  memory_bytes  = 8;
}

message StatusReport {
  repeated StackStatus stacks   = 1;
  int64    reported_at_ms       = 2;
}

message StackStatus {
  string stack_id          = 1;
  string running_version   = 2;
  repeated ContainerStatus containers = 3;
  bool   drift_detected    = 4;
  string drift_description = 5;
  string error             = 6;
}

message ContainerStatus {
  string container_id   = 1;
  string service_name   = 2;
  string state          = 3;  // running | exited | restarting | ...
  string status         = 4;  // "Up 3 hours", etc.
  int32  restart_count  = 5;
  int64  started_at_ms  = 6;
}

message LogChunk {
  string stream_id = 1;
  bytes  data      = 2;  // raw docker log bytes (may include stream prefix byte)
}

message StatsChunk {
  string stream_id                 = 1;
  repeated ContainerStats containers = 2;
}

message ContainerStats {
  string container_id  = 1;
  string service_name  = 2;
  double cpu_percent   = 3;
  int64  memory_usage  = 4;
  int64  memory_limit  = 5;
  int64  net_rx_bytes  = 6;
  int64  net_tx_bytes  = 7;
  int64  block_read    = 8;
  int64  block_write   = 9;
}

message CommandResult {
  bool   success = 1;
  string output  = 2;
  string error   = 3;
}

message DockerEvent {
  string type    = 1;  // container, image, network, volume
  string action  = 2;  // start, stop, die, oom, ...
  string actor_id = 3;
  map<string, string> attributes = 4;
  int64  timestamp_ms = 5;
}

message Pong { int64 timestamp_ms = 1; }

// ─── Enrollment ───────────────────────────────────────────────────────────────

message EnrollRequest {
  string bootstrap_token = 1;
  string csr_pem         = 2;  // PKCS#10 CSR
  Hello  node_info       = 3;
}

message EnrollResponse {
  string agent_id        = 1;
  string client_cert_pem = 2;
  string ca_bundle_pem   = 3;
}

message RenewCertRequest { string csr_pem = 1; }
message RenewCertResponse {
  string client_cert_pem = 1;
  string ca_bundle_pem   = 2;
}
```

---

### `stacks.proto` — UI API (Servers, Stacks, Deployments)

```protobuf
service StackService {
  // Servers
  rpc ListServers(ListServersRequest)   returns (ListServersResponse);
  rpc GetServer(GetServerRequest)       returns (Server);
  rpc UpdateServer(UpdateServerRequest) returns (Server);        // rename, labels
  rpc DeleteServer(DeleteServerRequest) returns (google.protobuf.Empty);

  // Stacks
  rpc ListStacks(ListStacksRequest)     returns (ListStacksResponse);
  rpc GetStack(GetStackRequest)         returns (Stack);
  rpc CreateStack(CreateStackRequest)   returns (Stack);
  rpc UpdateStack(UpdateStackRequest)   returns (Stack);         // creates new version
  rpc DeleteStack(DeleteStackRequest)   returns (google.protobuf.Empty);

  // Versions & Assignments
  rpc ListStackVersions(ListStackVersionsRequest) returns (ListStackVersionsResponse);
  rpc AssignStack(AssignStackRequest)             returns (Assignment);
  rpc UnassignStack(UnassignStackRequest)         returns (google.protobuf.Empty);
  rpc RollbackStack(RollbackStackRequest)         returns (Assignment); // re-assign old version

  // Container control (forwarded to agent)
  rpc ExecOnContainer(ExecOnContainerRequest) returns (ExecOnContainerResponse);

  // Live streams (server-streaming, bridged from agent)
  rpc StreamLogs(StreamLogsRequest)   returns (stream LogLine);
  rpc StreamStats(StreamStatsRequest) returns (stream ServerStats);
  rpc StreamEvents(StreamEventsRequest) returns (stream Event);
}
```

---

## Streaming Architecture

The Master acts as a **bridge** between browser streams and Agent streams:

```
Browser                    Master                     Agent
  │                          │                          │
  │── StreamLogs(req) ──────▶│                          │
  │                          │── LogRequest ───────────▶│ (via bidi stream)
  │                          │◀── LogChunk × N ─────────│
  │◀── LogLine × N ──────────│                          │
  │                          │                          │
  │── (disconnect) ─────────▶│                          │
  │                          │── CancelStream ─────────▶│
```

A `stream_id` (UUID) links `LogRequest`/`CancelStream` on the Agent side to the browser-facing
server-stream request. The Master uses a per-Agent `streamMux` map to route chunks to the right
waiting browser goroutine.

**Backpressure:** The Agent's `LogChunk`/`StatsChunk` goroutine blocks on the browser goroutine's
channel. If the browser is slow, the Agent slows down. This prevents unbounded buffering.

---

## Ports & Endpoints

| Port | Purpose |
|---|---|
| `4440` | Agent gRPC endpoint (mTLS, HTTP/2 only) — `4440` = orchestra concert pitch A440 |
| `8080` | UI API + SPA (TLS, Connect protocol, or plain HTTP behind a reverse proxy) |
| `9090` | Prometheus metrics (no auth, bind to loopback by default) |

`4440` and `8080` can be merged onto a single external port (e.g. `443`) with a
connection-level router (SNI passthrough for `4440`, TLS termination for `8080`). Default
config keeps them separate for simpler firewall rules. See `docs/08-deployment.md`.
