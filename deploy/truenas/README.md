# Deploying the orkestra Agent on TrueNAS SCALE

TrueNAS SCALE 24.10+ ("Electric Eel" and later) runs apps on **Docker**. There are two ways to
run the orkestra Agent here.

The agent connects **outbound** to the Master over mTLS and needs **no inbound ports**. It
auto-enrolls on first boot from `ORKESTRA_MASTER_ADDR` + `ORKESTRA_BOOTSTRAP_TOKEN`, then reuses
the stored certificate on every restart. Its data dir (`/var/lib/orkestra`) must be on persistent
storage.

## Prerequisites (both paths)

1. In the orkestra **Master UI**, create an **enrollment token** and copy it.
2. Your Master's agent endpoint on port **`4440`**, e.g. `https://orkestra.example.com:4440`
   (must be reachable from the TrueNAS host; the hostname must be in the Master's
   `ORKESTRA_AGENT_TLS_SANS`).
3. A dataset for persistence, e.g. `/mnt/<pool>/apps/orkestra-agent`.

---

## Path A — Custom App (fastest, always works)

Apps → **Discover Apps** → **Custom App** → **Install via YAML**, then paste
[`custom-app.yaml`](./custom-app.yaml) after filling in the three placeholders (master URL,
token, dataset path). Done.

This is the recommended path if you just want it running.

## Path B — Guided catalog app (labeled form + native "Update" button)

The [`catalog/`](./catalog/) directory is a self-hosted TrueNAS **train** with a guided install
form ([`questions.yaml`](./catalog/stable/orkestra-agent/0.1.0/questions.yaml)).

1. Host the `catalog/` directory in a git repository (or point at this repo's subpath).
2. On TrueNAS: Apps → **Discover Apps** → **⋮** → **Manage Catalogs** → **Add Catalog**, and give
   the git repo URL with train `stable`.
3. Install **orkestra Agent** from the catalog and fill in the form.

### Caveat — validate against your TrueNAS version

The TrueNAS app schema (the ix "app template" format, `app.yaml` / `questions.yaml` /
`templates/docker-compose.yaml`) is **version-sensitive**, and the middleware may require fields
this skeleton omits (notably `lib_version` / `lib_version_hash` pinning the shared ix library).
This app uses a **self-contained** compose template (no ix-lib helpers) to stay portable, but you
should validate it on your actual TrueNAS box before relying on it — TrueNAS surfaces schema
errors when adding the catalog or installing. If the catalog rejects the app, use **Path A**
(Custom App), which does not depend on the catalog schema.

---

## Docker socket permissions

The agent needs `/var/run/docker.sock`, which is `root:root` on TrueNAS. Both paths run the
container as root (`user: "0:0"`) for reliable access. If your socket is group-accessible, you can
instead drop the root user and add `group_add: ["<docker-gid>"]`.
