# Roadmap

Status as of 2026-04-30.

## v0.1.0 — Initial release (shipped)

The reference Go implementation of the [agentskills.io open
standard](https://agentskills.io). Public API surface targets full
conformance with the spec plus Claude Code extensions.

| Task | Component | Status |
|------|-----------|--------|
| A | `frontmatter.go` + `metadata.go` — YAML/JSON frontmatter + typed Metadata + `ValidateName` | shipped |
| B | `embedded.go` — single-skill loader with `//go:embed` + env-override + mtime cache (Pattern A) | shipped |
| C | `tier.go` — `Resolver`/`ContextResolver`/`DirTier`/`EmbedFSTier`/`PluginTier` | shipped |
| D | `catalog.go` + `summary.go` — multi-tier catalog with XML/Markdown/JSON summaries (Pattern B) | shipped |
| E | `bootstrap.go` — `InitWorkspace` first-run defaults | shipped |
| F | `doc.go` + `doc/skill.md` + README polish | shipped |
| - | Final code-quality review | shipped |

Acceptance: ≥90% line coverage, `go test -race` clean,
`golangci-lint v2` clean, `go vet` clean, stdlib-only deps.

Implementation plan: [`plans/2026-04-30-init.md`](plans/2026-04-30-init.md).

## v0.2.0 — Observability (shipped)

Single-feature release per the senior-judgment default rule: land one
well-scoped addition, validate it in production via MemDB consumer
adoption, then continue with remaining hardening items.

| Task | Component | Status |
|------|-----------|--------|
| Observer hooks | `observer.go` + `embedded.go` + `catalog.go` — vendor-neutral `Observer` struct with nil-able func fields | **shipped** |

### What shipped

- `Observer` struct with hooks: `BodyCall`, `EnvFallback`, `BodyBytes`,
  `CatalogLoad`, `CatalogSize`. All fields optional; nil = no-op.
- `WithObserver(*Observer) EmbeddedOption` — attach at `NewEmbedded`.
- `NewEmbedded(name, envVar, raw, opts ...EmbeddedOption)` — variadic;
  existing 3-arg call sites unchanged (backward compatible).
- `(*Catalog).WithObserver(*Observer) *Catalog` — chained method; fires
  `CatalogSize` once per tier on first attach.
- All resolution paths in `Embedded.Body()` instrumented: embedded
  fast-path, env-success, cache-hit, last-known-good (stat failure +
  ReadFile failure), three env-fallback reasons.
- `Catalog.Load` / `LoadCtx` instrumented with hit/miss outcome.
- `LoadMany` / `LoadManyCtx` intentionally NOT instrumented — they are
  batch wrappers; the per-name `Load` call is the right granularity.

### Design rationale

Observer fields are nil-able function values, not an interface. This
means:

1. Adding a new field in a future release is non-breaking — existing
   callers' `Observer` literals compile unchanged (new field defaults
   to nil = no-op).
2. Removing a field is a breaking change reserved for v2.0.0.
3. No prometheus / otel transitive dep in skillkit core; consumers wire
   to whichever backend they already have.

See `doc/skill.md § 11. Observability` for wiring examples.

## v0.2.1 — Deferred hardening items

The items below were originally listed as v0.2.0 candidates but are
deferred until observability is validated in production (MemDB consumer
adoption). Single-feature-release rule applied.

- **Locale routing** — promote `Locale` from a metadata field to a
  first-class `Catalog.WithLocale(locale)` filter. Lift from internal
  practice once at least 2 consumers want it.
- **Skill validation CLI** — small `cmd/skillvalidate` binary mirroring
  the conformance checks of `agentskills/skills-ref`. Useful for CI
  integration in consumer repos.
- **Description compose helper** — concat `Description` + `WhenToUse`
  per Claude Code's discovery rule (combined ≤1536 chars per spec).
  Currently consumers do this themselves.
- **`AllowedTools` matcher** — `Metadata.AllowsTool(name)` helper that
  enforces the whitelist when an agent dispatches.

## v0.3.0 — Network-backed resolvers

`ContextResolver` shipped in v0.1.0 specifically to leave the door open
for these without breaking change. Each is its own focused module
inside the same repo:

- **MCPResolver** — wraps an MCP `prompts/list` + `prompts/get` client.
  When a connected MCP server publishes a skill, it appears in
  `Catalog.List()` automatically; `notifications/prompts/list_changed`
  invalidates the cache. The Go MCP SDK
  (`github.com/modelcontextprotocol/go-sdk`) already exposes
  `Server.AddPrompt` with auto-notification — wiring is straightforward.
- **LangfuseResolver** — for teams with prompt versioning + A/B in
  Langfuse. Resolver pulls labelled `production` prompts, mtime-equivalent
  invalidation via Langfuse's update timestamp.
- **HTTPResolver** — generic HTTP-fetched skill bodies for sidecar
  patterns.

These ship as separate sub-packages (e.g. `mcpresolver/`) with their
own go.mod under the same repo so the core package stays stdlib-only.

## v1.0.0 — Public API stability

Tag v1.0.0 once at least 5 of the 7 internal consumers have migrated
and we have at least 3 months of operational data on cache invalidation
edge cases (NFS, bind mounts, fsnotify-vs-mtime in production). Before
v1.0.0, public API may still change.

Stability guarantees at v1.0.0:

- All exported identifiers in `package skill` follow strict semver
  (no breaking changes within v1.x.y)
- `agentskills.io` spec conformance is a CI gate (validated via
  `skills-ref` against test fixtures)
- Deprecation policy: minimum 2 minor releases between deprecation
  marker and removal

## Anti-roadmap (deliberately out of scope)

- **Skill installation / publishing** — vaelor has `pkg/skills/installer.go`
  for git-clone-style installs. That belongs in a separate CLI tool, not
  in this loader library.
- **Skill execution** — running scripts from `scripts/` is the consuming
  agent's responsibility. We expose `SkillInfo.Dir`; the agent decides
  what to do with it.
- **fsnotify integration** — inotify-style file watchers are unreliable
  on NFS, SMB, and several Docker bind-mount configurations. mtime
  polling at request time is intentionally chosen and will not change.
- **Custom YAML parser improvements** — the loader's permissive parser
  handles top-level scalars + flow lists + `metadata:` flattening, by
  design. Consumers needing arbitrary nested YAML should use a
  dedicated YAML library on the raw frontmatter (available via
  `ParseFrontmatter`).
- **Schema validation beyond the spec** — `ValidateName` enforces the
  agentskills.io rules. Custom field validation is the consumer's
  concern.

## Tracking

Plans for each release live in [`plans/`](plans/). Each plan is a
self-contained spec for one release scope.
