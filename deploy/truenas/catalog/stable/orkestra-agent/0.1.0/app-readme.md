# orkestra Agent

Runs the orkestra Agent on this TrueNAS host. The agent connects **outbound** to your orkestra
Master over mTLS and reconciles Docker Compose stacks toward the desired state — no inbound ports
are required on this machine.

## Before you install

1. In the orkestra **Master UI**, create an **enrollment token** and copy it.
2. Note your Master's agent URL, e.g. `https://orkestra.example.com:4440` (port **4440**).

## Fields

- **Master URL** — your Master's agent endpoint on port `4440`.
- **Bootstrap Token** — the one-time enrollment token. Used only on first boot; afterwards the
  agent reuses the certificate it stored.
- **Persistent Storage** — mounted at `/var/lib/orkestra`; holds the enrollment certificate. Must
  persist across restarts.
- **Docker Socket Host Path** — defaults to `/var/run/docker.sock` (correct for standard TrueNAS).

The agent runs as root to access the Docker socket. Metrics are federated through the Master, so
no metrics port needs to be exposed here.
