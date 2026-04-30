# Roadmap

Status as of 2026-04-30.

## v0.1.0 — Initial release (in progress)

The reference Go implementation of the [agentskills.io open
standard](https://agentskills.io). Public API surface targets full
conformance with the spec plus Claude Code extensions.

| Task | Component | Status |
|------|-----------|--------|
| A | `frontmatter.go` + `metadata.go` — YAML/JSON frontmatter + typed Metadata + `ValidateName` | planned |
| B | `embedded.go` — single-skill loader with `//go:embed` + env-override + mtime cache (Pattern A) | planned |
| C | `tier.go` — `Resolver`/`ContextResolver`/`DirTier`/`EmbedFSTier`/`PluginTier` | planned |
| D | `catalog.go` + `summary.go` — multi-tier catalog with XML/Markdown/JSON summaries (Pattern B) | planned |
| E | `bootstrap.go` — `InitWorkspace` first-run defaults | planned |
| F | `doc.go` + `doc/skill.md` + README polish | planned |
| - | Final code-quality review | planned |

Acceptance: ≥90% line coverage, `go test -race` clean,
`golangci-lint v2` clean, `go vet` clean, stdlib-only deps.

Implementation plan: [`plans/2026-04-30-init.md`](plans/2026-04-30-init.md).

## v0.2.0 — Real-world hardening

After v0.1.0 ships, gather migration feedback from the consumer repos
before locking the public API. Likely additions:

- **Locale routing** — promote `Locale` from a metadata field to a
  first-class `Catalog.WithLocale(locale)` filter. Currently
  out-of-scope; only consumer pattern is MemDB's stale `.ru.md`/`.zh.md`
  files which the loader doesn't yet route. Lift from internal practice
  once at least 2 consumers want it.
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
