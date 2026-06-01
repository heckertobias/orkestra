# orkestra

Lightweight orchestrator for Docker/Compose hosts — a simpler alternative to Kubernetes or Nomad when all you need is centrally managed, self-healing Compose stacks across multiple Linux servers.

## Why orkestra?

Plain "SSH + docker compose" is not centrally controllable, not self-healing, and not auditable. Kubernetes and Nomad are too heavy for standalone Docker hosts. orkestra fills this gap.

- A lightweight **Agent** (single Go binary, no runtime) runs on every Linux server and manages containers via the Docker Engine API.
- A central **Master** holds Desired State in SQLite, distributes it to Agents, and exposes a **Web UI**.
- Agents connect **outbound** to the Master — NAT and firewall friendly, authenticated via **mTLS**.

## Architecture

```
                     ┌──────────────────────────────────────────┐
                     │                 MASTER                    │
 Browser  ── HTTPS ─▶│  ┌────────────┐   ┌──────────────────┐   │
 (React SPA)         │  │  HTTP/API  │   │   Reconciler /   │   │
                     │  │ (Connect)  │◀─▶│   Scheduler      │   │
                     │  └────────────┘   └──────────────────┘   │
                     │  ┌────────────┐   ┌──────────────────┐   │
                     │  │ Agent-gRPC │   │  Store (SQLite)  │   │
                     │  │  Endpoint  │   │  + CA / PKI      │   │
                     │  └─────┬──────┘   └──────────────────┘   │
                     └────────┼─────────────────────────────────┘
          mTLS / gRPC bidi-stream │  (Agent connects outbound)
            ┌──────────────┬──────┴───────┬──────────────┐
            ▼              ▼              ▼              ▼
      ┌──────────┐   ┌──────────┐   ┌──────────┐   ┌──────────┐
      │  AGENT   │   │  AGENT   │   │  AGENT   │   │  AGENT   │
      │ Server A │   │ Server B │   │ Server C │   │ Server D │
      │ Docker   │   │ Docker   │   │ Docker   │   │ Docker   │
      └──────────┘   └──────────┘   └──────────┘   └──────────┘
```

## Core Principles

- **Single source of truth** — Master holds Desired State in SQLite; Agents are stateless with respect to configuration.
- **Connect-out** — Agents initiate the connection; the Master never dials out. NAT/firewall friendly.
- **Reconciliation over imperative** — Agents continuously converge toward desired state and report drift back.

## Tech Stack

| Layer | Choice |
|---|---|
| Language | Go ≥ 1.24 |
| RPC / API | ConnectRPC (gRPC + browser-native, one schema) |
| Docker control | Docker Engine SDK + compose-go |
| Persistence | SQLite (CGo-free) + sqlc + goose migrations |
| Auth | argon2id (local) + OIDC (optional) |
| Secrets | Built-in (age/NaCl) or OpenBao (Vault-compatible) |
| Frontend | React + TypeScript + Vite + Tailwind + TanStack Query |
| Packaging | goreleaser — single binary, systemd units, Docker image |

The React SPA is embedded into the Master binary via `go:embed` — one artifact serves both API and UI.

## Quick Start

### Master (Docker Compose)

```bash
export ORKESTRA_MASTER_KEY=$(openssl rand -hex 32)
echo "ORKESTRA_MASTER_KEY=$ORKESTRA_MASTER_KEY" >> .env
# Store the key somewhere safe — it encrypts all secrets at rest.

docker compose -f deploy/docker/compose.yaml up -d
docker compose -f deploy/docker/compose.yaml logs master | grep "setup"
# Open the setup URL to create the first admin user.
```

### Agent

```bash
./deploy/install-agent.sh \
  --master https://master.example.com:8443 \
  --bootstrap-token <token> \
  --name "web-server-01"
```

The script downloads the agent binary, enrolls it (PKI/mTLS), installs and starts the systemd service.

## Development

```bash
# Prerequisites: Go 1.24+, buf, sqlc, Docker, Node 20+

make proto    # buf generate → Go + TypeScript stubs
make sqlc     # sqlc generate → type-safe DB layer
make build    # go build ./cmd/...
make test     # go test ./...
make web      # npm run build in web/
```

## Project Status

orkestra is in early development (Milestone 0 — scaffolding complete). See [docs/09-roadmap.md](docs/09-roadmap.md) for the full implementation roadmap.

| Milestone | Description | Status |
|---|---|---|
| M0 | Repo scaffolding & tooling | ✅ Done |
| M1 | PKI, enrollment, persistent mTLS connection | 🔧 Next |
| M2 | Container control & minimal Web UI | Planned |
| M3 | Compose stacks & desired-state reconciliation | Planned |
| M4 | Secrets (built-in + OpenBao) | Planned |
| M5 | User auth, sessions & RBAC | Planned |
| M6 | OIDC, metrics, event feed & polish | Planned |
| M7 | Hardening, packaging & v0.1.0 release | Planned |

## Documentation

- [docs/00-overview.md](docs/00-overview.md) — Architecture overview
- [docs/01-repo-layout.md](docs/01-repo-layout.md) — Repository structure & build tooling
- [docs/02-protocol.md](docs/02-protocol.md) — gRPC/Connect protocol
- [docs/03-data-model.md](docs/03-data-model.md) — SQLite schema
- [docs/04-reconciliation.md](docs/04-reconciliation.md) — Desired-State model & Converge Engine
- [docs/05-secrets.md](docs/05-secrets.md) — SecretProvider, built-in, OpenBao
- [docs/06-security-auth.md](docs/06-security-auth.md) — PKI/mTLS, user auth, RBAC, audit
- [docs/07-web-ui.md](docs/07-web-ui.md) — UI pages & frontend stack
- [docs/08-deployment.md](docs/08-deployment.md) — Observability & deployment
- [docs/09-roadmap.md](docs/09-roadmap.md) — Implementation milestones

## License

MIT — see [LICENSE](LICENSE).
