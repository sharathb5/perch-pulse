# AGENTS.md — rules for Cursor and Codex

This repository is **Perch Pulse 2.0**, extending open-source Perch. These rules bind **Cursor** (primary implementer) and **Codex** (independent reviewer). Humans supervising agents share the same constraints.

Product: [`docs/PRODUCT.md`](docs/PRODUCT.md)  
Architecture: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md)  
Decisions: [`docs/DECISIONS.md`](docs/DECISIONS.md)

## Roles

| Agent | Role |
|-------|------|
| Cursor | Implement changes, run verification, open PRs |
| Codex | Read-only review of reliability, security, and test gaps; do not create recursive agent loops |
| GitHub Actions | Authoritative merge gate via `make verify` |

## Non-negotiable invariants

1. **Stale ≠ healthy.** Stale or unavailable telemetry must never be represented as current healthy status.
2. **Event integrity.** Duplicate and out-of-order events must not silently corrupt system state.
3. **Evidence vs inference.** AI-generated diagnoses must distinguish evidence from inference.
4. **Correlation ≠ causation.** Correlation must never be presented as proven causation.
5. **Secret hygiene.** Provider credentials must never be exposed in logs, test output, or agent context.
6. **Bounded I/O.** External API calls must have explicit timeouts and error handling.
7. **Databricks optional.** Basic Perch functionality must not depend on Databricks availability.
8. **No autonomous destruction.** Agents cannot deploy, merge, force-push, or perform destructive operations without explicit human approval.

Enforce these with executable checks where practical (tests, lint, gosec, verify). Do **not** invent placeholder Pulse product features to “satisfy” an invariant on paper.

## Architectural boundaries

| Module area | Own | Do not |
|-------------|-----|--------|
| `cmd/perch`, `internal/cli` | CLI surface, viz server | Business logic that belongs in collectors |
| `internal/config` | `perch.yaml` load/validate | Provider API calls |
| `internal/provider`, `providers/` | Specs and registry | UI concerns |
| `internal/stackstatus`, `stacklogs`, `graph` | Live collectors | Warehouse/Pulse history (future `internal/pulse/…`) |
| `internal/credentials` | Local secret store | Printing secrets |
| `web/` | React viz UI | Direct filesystem credential access |
| Docs under `docs/` | Specs and ADRs | Claiming planned features exist |

Future Pulse modules must remain optional relative to core Perch collectors.

## Coding conventions

### Go

- Module path: `github.com/yashg4509/perch` until an ADR migrates it.
- Match neighboring package layout (`internal/<pkg>/test` where already used).
- `gofmt` clean; no network in unit tests unless faked.
- Build `web/dist` before compiling packages that import `web` (`make web-build-embed` or `make verify`).

### Frontend (`web/`)

- Stay aligned with viz APIs: `/api/graph`, `/api/status`, `/api/logs`, `/api/credentials`, `/api/pulse/*`.
- Prefer existing hooks/mappers over parallel data paths.
- Use `npm ci` for reproducible installs; do not commit `node_modules` or `dist`.
- Preserve the existing UI: [`web/DESIGN.md`](web/DESIGN.md). Verification loop: [`web/UI_VERIFICATION.md`](web/UI_VERIFICATION.md). Skills: `.cursor/skills/perch-ui-implementation/`, `.cursor/skills/perch-ui-review/`.
- Browser checks: `make web-e2e` (separate from `make verify`; Chromium + fixtures only).

### Providers

- Add YAML under `providers/<category>/` from `_template.yaml`.
- Validate with `make provider-validate`.

### License

- Preserve MIT license and attribution in [`LICENSE`](LICENSE).

## Security requirements

- Never commit secrets, tokens, private keys, or real `.env` files.
- Credentials live in `~/.perch` on the developer machine only.
- Do not weaken gosec/govulncheck excludes without an ADR and justification.
- Viz remains localhost-oriented; do not expose credential APIs beyond loopback without explicit design + review.
- Redact secrets from logs, errors, fixtures, and agent-facing JSON.

## Testing and verification

Before claiming work ready:

```bash
make verify
```

Fail-fast; nonzero exit on any required check. Individual targets (`make web-lint`, `make go-test`, `make security`, …) may be used during iteration, but **PR completion requires full `make verify`**.

Do not claim verification passed unless the commands actually succeeded.

## PR completion requirements

1. Branch off current `main`; do not rewrite shared history.
2. `make verify` green locally.
3. CI green on the PR.
4. Meaningful description: intent, risk, test plan.
5. Update `docs/DECISIONS.md` for non-obvious tradeoffs.
6. No secrets in the diff.
7. Do **not** merge, deploy, or change branch protection unless a human explicitly asks.

## Documenting tradeoffs

- Record durable choices in [`docs/DECISIONS.md`](docs/DECISIONS.md) using the ADR template there.
- Prefer linking evidence (failing test, gosec finding, benchmark) over opinion.
- Distinguish **existing** vs **planned** behavior in docs; never imply Pulse modules exist before code lands.

## Ambiguous specifications

When requirements conflict or lack acceptance criteria:

1. Prefer preserving existing Perch behavior.
2. Choose the smallest reversible change.
3. Ask the human for consequential product/architecture calls (new backends, module path renames, auth model changes).
4. Document assumptions in the PR and, if durable, an ADR.
5. Stop for credential access, destructive git, deployment, or irreversible cloud changes.

## Upstream remotes

- `origin` → `sharathb5/perch-pulse`
- `upstream` → `yashg4509/perch` (optional sync; do not modify the upstream GitHub repo)

## Codex usage

- Prefer read-only / sandbox review of diffs and infrastructure.
- One-shot reviews; no recursive `codex` → `codex` loops.
- If Codex is unavailable or unauthenticated, document the blocker in the PR and continue.
- Never pass secrets into Codex prompts.
