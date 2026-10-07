# Architecture — current Perch vs proposed Pulse

Separate **what exists today** from **proposed Pulse modules**. Proposed sections are forward design only; do not treat them as implemented.

## Part A — Current architecture (imported Perch)

### Overview

Go CLI + optional embedded React SPA. Reads `perch.yaml`, loads embedded provider YAML, builds a dependency graph, probes health when credentials allow, and resolves logs through an ordered credential strategy chain. No Perch cloud service.

```mermaid
flowchart TB
  subgraph local["Developer machine"]
    YAML["perch.yaml"]
    Creds["~/.perch/credentials\n+ project .env"]
    CLI["perch CLI / TUI"]
    Viz["perch viz\n127.0.0.1"]
    YAML --> CLI
    Creds --> CLI
    CLI --> Viz
  end

  subgraph core["Go core"]
    CFG["internal/config"]
    REG["internal/provider + providers/**"]
    G["internal/graph"]
    SS["internal/stackstatus"]
    SL["internal/stacklogs"]
    CTX["internal/stackcontext"]
  end

  subgraph ui["Embedded web UI"]
    SPA["web/dist via go:embed"]
    API["/api/graph /status /logs /credentials"]
    SPA --> API
  end

  CLI --> core
  Viz --> API
  API --> G
  API --> SS
  API --> SL
  Vendors["Provider APIs / CLIs"] --> SS
  Vendors --> SL
```

### Module ownership (today)

| Area | Path | Responsibility |
|------|------|----------------|
| Entrypoint | `cmd/perch` | Binary → `cli.Execute` |
| CLI | `internal/cli` | Cobra commands, viz HTTP server |
| Config | `internal/config` | Load/validate `perch.yaml` |
| Providers | `providers/`, `internal/provider`, `internal/providerspec` | Specs, registry, validation |
| Graph | `internal/graph` | Environment topology |
| Status | `internal/stackstatus`, `internal/customstatus` | Probes + custom shell health |
| Logs | `internal/stacklogs`, `internal/customlogs` | Credential chain + fetch |
| Context | `internal/stackcontext`, `internal/llm` | Merged / agent summaries |
| Credentials | `internal/credentials` | Local store + `.env` sync |
| TUI | `internal/tui` | Bubbletea UI |
| Web | `web/`, `web/embed.go` | Vite React app embedded for viz |

### Data flow (today)

1. Discover `perch.yaml` from CWD upward.
2. Load provider registry (embed or `PERCH_PROVIDERS_DIR`).
3. Build graph for `--env`.
4. Collect status/logs via provider APIs, CLIs, or custom shell—with timeouts where implemented.
5. Serve the same collectors over localhost for the SPA, or print JSON/TUI for humans/agents.

### Runtime constraints

- Viz binds to loopback; credentials stay on the machine.
- `//go:embed` requires `web/dist` before Go packages importing `web` compile (`npm run build:embed`).
- Status/logs are **point-in-time**, not warehouse history.

See [`CODEBASE_GUIDE.md`](CODEBASE_GUIDE.md) for flows and known bug surfaces.

---

## Part B — Pulse architecture

Keep Pulse modular and optional relative to Part A. Prefer packages under `internal/pulse/...` over overloading `stackstatus`.

### Implemented

| Module | Path | Intent |
|--------|------|--------|
| Observation contract | `internal/pulse/observation` | Typed service signal with explicit timestamps and derived freshness (`IsStale` / `EffectiveStatus`). No I/O; collectors not yet wired. |

### Proposed (not implemented)

```mermaid
flowchart LR
  subgraph existing["Existing collectors"]
    SS["stackstatus"]
    SL["stacklogs"]
    G["graph"]
  end

  subgraph pulse["Pulse"]
    OBS["observation (implemented)"]
    INV["Investigation / evidence"]
    BASE["Baseline store"]
    DEP["Deploy impact"]
    AGENT["Agent context packager"]
  end

  subgraph backends["Retention backends"]
    LOCAL["Local/file retention"]
    DBX["Databricks optional"]
  end

  SS -.-> OBS
  SL -.-> OBS
  OBS --> INV
  SS --> BASE
  DEP --> INV
  INV --> AGENT
  BASE --> LOCAL
  BASE -.-> DBX
  DEP -.-> DBX
```

| Module | Intent | Databricks |
|--------|--------|------------|
| Evidence / investigation | Assemble citations from collectors + topology | Not required for assembly |
| Baselines | Compare current signals to history | Optional amplify |
| Deploy impact | Correlate deploys with health/log shifts | Optional for large joins |
| Agent context packager | Stable schemas; evidence vs inference | Not required |
| Databricks backend | Warehouse-scale analytics | **Optional only** |

### Integration rules

1. Part A collectors remain the live local source of truth.
2. Pulse consumes collector outputs; it does not replace `perch.yaml` or provider YAML.
3. Unset optional backends → explicit unavailable semantics, never fake-healthy status.
4. Do not implement these modules in Phase 0.

---

## Part C — Verification architecture

| Concern | Mechanism |
|---------|-----------|
| Local gate | `make verify` |
| PR / main gate | `.github/workflows/ci.yml` → `make verify` |
| Release | `.github/workflows/release.yml` (unchanged; no deploy from Phase 0) |

Agents treat `make verify` as the definition of “buildable.”
