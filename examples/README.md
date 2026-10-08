# Manual test workspace for perch

This directory holds **copy-paste-friendly sample projects** so you can exercise the CLI without touching a real production repo. It is designed to:

- Cover **init detection**, **`perch.yaml`**, **`status`**, **`graph`**, **`context`**, and the **interactive TUI**
- Stay **easy to fork** into a standalone repository later (see [Using as a separate repo](#using-as-a-separate-repo))
- Stay **easy to extend** (see [Adding a new scenario](#adding-a-new-scenario))

## Prerequisites

- **Go** — same major.minor as the root `go.mod` (see repository root).
- **Built binary** — from the **repository root** (not from inside a scenario folder):

  ```bash
  cd /path/to/perch
  go build -o perch ./cmd/perch
  ```

  Optionally add the binary to your `PATH`, or invoke it with a full path.

- **Provider definitions** — by default, perch uses YAML **embedded in the binary**. You only need the repo’s `providers/` directory if you set `PERCH_PROVIDERS_DIR` (useful when hacking provider YAML without rebuilding):

  ```bash
  export PERCH_PROVIDERS_DIR=/path/to/perch/providers
  ```

## Scenarios

| Folder | Purpose |
|--------|---------|
| [`scenarios/init-signals`](scenarios/init-signals) | **Detection only** — run `perch init` here to regenerate `perch.yaml` from `vercel.json` + `package.json`. |
| [`scenarios/full-stack`](scenarios/full-stack) | **Committed `perch.yaml`** — run `status`, `graph`, `context`, and the TUI without running `init` first. Optional local health server for `custom` dev commands. |
| [`scenarios/manual-cli-test`](scenarios/manual-cli-test) | **Two local health servers** — best default for exercising `dev` custom nodes, `graph`/`status`/`context`, and the TUI together (see scenario README). |
| [`scenarios/full-platform`](scenarios/full-platform) | **Perch Brief** — 5‑min app + Perch CLI demo; one guide: [`README.md`](scenarios/full-platform/README.md). |
| [`astronomy-shop`](astronomy-shop) | **Phase 2 target** — clone/start OpenTelemetry Astronomy Shop (pin 3.1.0) and map topology to Pulse IDs; see [`README.md`](astronomy-shop/README.md) and [`docs/astronomy-shop.md`](../docs/astronomy-shop.md). |
| [`scenarios/_template`](scenarios/_template) | Checklist for **adding** a new scenario. |

## Quick commands (after `cd` into a scenario)

Replace `./perch` with the path to your built binary.

```bash
# Graph topology (JSON)
./perch graph --json

# Edges in perch.yaml (no hand-editing)
./perch edge list
# ./perch edge add web api
# ./perch edge rm web api --dry-run

# Status for current env (--env defaults to production)
./perch status
./perch status --json

# Agent-oriented context
./perch context --json
./perch context --for-agent

# Interactive TUI — graph, health-colored ●, footer; ? palette · E next env · arrows + Enter · q quit
./perch

# Switch environment (CLI flag, or press E inside TUI when multiple envs exist)
./perch --env dev status --json
```

## Third-party services (optional)

Nothing in this folder **requires** paid cloud accounts for basic CLI exercises: read-only nodes use **stubbed** JSON in the current milestone, and deployable nodes may show **placeholder** health until real API wiring is complete.

The repo **bundles** many provider definitions under root [`providers/`](../providers/) (Vercel, Render, Supabase, Stripe, OpenAI, …). See **[docs/providers.md](../docs/providers.md)** for the **full list**, **init detection** rules (`package.json` + config files), and notes on **`next-auth`** / **`langchain` → `langsmith`**.

To test **real** integrations when you are ready:

1. Set the resource fields in `perch.yaml` (`project`, `service`, …) to match your account.
2. Add credentials under **`~/.perch/credentials`** using the **`credentials.key`** from the matching file under `providers/<category>/<name>.yaml` (JSON object of string keys to string secrets).
3. Rebuild the binary after editing embedded YAML, or set **`PERCH_PROVIDERS_DIR`** to the repo’s `providers/` folder while iterating.

| Example | Provider id | Credential key (see YAML) |
|---------|-------------|---------------------------|
| **Vercel** | `vercel` | `vercel_token` |
| **OpenAI** | `openai` | `openai_api_key` |

Do **not** commit secrets; example `perch.yaml` files use placeholders like `YOUR_VERCEL_PROJECT` or `CHANGE_ME`.

## Using as a separate repo

1. Create a new empty repository (e.g. `perch-fixtures`).
2. Copy the `examples/` tree (or only `examples/scenarios/`) into it.
3. In the fixture repo’s README, tell users to build perch from **github.com/yashg4509/perch** and either install the binary on `PATH` or reference it by path.
4. When provider YAML changes in the main repo, either rebuild perch or set `PERCH_PROVIDERS_DIR` to a checkout of `perch/providers`.

Keeping scenarios **self-contained** (each folder has its own `perch.yaml` or init inputs) makes that split straightforward.

## Adding a new scenario

Follow the checklist in [scenarios/_template/README.md](scenarios/_template/README.md), then add a row to the **Scenarios** table above.

## Troubleshooting

- **`unknown environment`** — Use `--env` that exists in `perch.yaml` (`production`, `staging`, `dev` in `full-stack`).
- **`unknown provider`** — Rebuild perch after adding or moving YAML under `providers/`, or set `PERCH_PROVIDERS_DIR`.
- **TUI in automation** — The root `perch` command is interactive; automated tests use `internal/tui` package tests instead.
