# Architecture

This document describes the design of `agentskills-go` — what the
package contains, why it is structured this way, and how the pieces
fit together.

## Goals

1. **Spec conformance.** A skill validated by the official
   `agentskills/agentskills` `skills-ref` CLI must load correctly
   through this package. Frontmatter field names match the spec
   exactly; name validation matches the spec rules.
2. **Two patterns, one package.** Single-skill embedded binaries
   (Pattern A) and multi-skill agent CLIs (Pattern B) have different
   concerns. We expose them as independent APIs that share frontmatter
   primitives — consumers pay only for what they import.
3. **Stdlib-only.** Zero external runtime dependencies. The loader is
   a low-level utility that tens of services may depend on; adding a
   transitive dependency to all of them is not free. Tests use
   `testing` plus stdlib.
4. **Forward-compatible extension.** New skill sources (MCP servers,
   Langfuse, HTTP) plug in as `Resolver` implementations without
   breaking changes to existing consumers.
5. **Operator-friendly hot-reload.** Edit a skill `.md` file on disk;
   the next request picks it up. No restarts during prompt iteration.
   No fsnotify dep (unreliable on NFS / Docker bind mounts).

## Non-goals

- Skill installation, publishing, or registry — separate concerns.
- Skill execution (running `scripts/`) — caller's job.
- Generic YAML parser — we ship a permissive minimal parser sufficient
  for the spec; consumers needing arbitrary YAML can use
  `ParseFrontmatter` and run a real YAML library on the raw block.

## Two patterns

### Pattern A — `Embedded` (single-skill binaries)

Used when a service ships exactly one skill, baked into the binary
via `//go:embed`. Optional env-path override allows operators to
hot-reload during prompt iteration.

```
┌────────────────────────────────────────────────────────────┐
│ Service binary                                             │
│                                                            │
│  ┌─────────────────────┐                                   │
│  │ //go:embed skill.md │ ──parsed once──> Embedded.default │
│  │ rawSkill string     │                                   │
│  └─────────────────────┘                                   │
│                                                            │
│  Body() ──> os.Stat(envPath)                               │
│              │                                             │
│              ├─ ok + ≤1MiB + non-empty body ──> mtime-cached
│              │                                             │
│              └─ otherwise ──> Embedded.default             │
└────────────────────────────────────────────────────────────┘
```

Resolution per `Body()` call:

1. If the configured env var holds a readable path, the file is ≤1 MiB,
   and the stripped body is non-empty → mtime-cached body wins.
2. Otherwise → embedded default (frontmatter pre-stripped at construction).

`Body()` always returns a non-empty string. `NewEmbedded` panics if
the embedded raw has an empty body — build-time invariant, not a
runtime error.

Use cases inside our ecosystem:
- D10 answer extractor in MemDB (`memdb-go/internal/search/`)
- Atomic-fact extractor in MemDB (`memdb-go/internal/llm/`)
- 36+ hardcoded prompt constants across go-job, go-code, go-search,
  go-pentest (Phase 2 migration candidates)

### Pattern B — `Catalog` + `Resolver` (multi-skill agents)

Used when an agent CLI loads many skills, typically across tiered
sources (workspace user dirs > global > builtin > plugin), and injects
a summary into its system prompt.

```
┌────────────────────────────────────────────────────────────┐
│ Agent CLI                                                  │
│                                                            │
│  Catalog                                                   │
│   ├── Tier{Name: "workspace", Resolver: DirTier(...)}     │
│   ├── Tier{Name: "global",    Resolver: DirTier(...)}     │
│   ├── Tier{Name: "builtin",   Resolver: EmbedFSTier(...)} │
│   └── Tier{Name: "plugin",    Resolver: PluginTier(...)}  │
│                                                            │
│  Catalog.Load("doc-review")                                │
│   └── walks tiers in order, returns first ok=true          │
│                                                            │
│  Catalog.BuildSummary(SummaryXML)                          │
│   └── <skills><skill name=…><description>…</description>…  │
│       <skill-group plugin="foo">…</skill-group></skills>   │
└────────────────────────────────────────────────────────────┘
```

`Resolver` is the extension point. Three built-in implementations
cover all known consumer patterns:

| Resolver | Source | Use case |
|----------|--------|----------|
| `DirTier` | filesystem `<dir>/<skill>/SKILL.md` | dozor workspace + builtin, vaelor multi-tier, go-hully, go-wp |
| `EmbedFSTier` | `embed.FS` rooted at `<root>/<skill>/SKILL.md` | binaries shipping a fixed multi-skill catalog |
| `PluginTier` | `[]PluginEntry` with namespacing | vaelor plugin sources |

`ContextResolver` is an optional interface (`FindCtx(ctx, name)`) for
network-backed resolvers (MCP servers, Langfuse, HTTP). Catalog
detects implementation via type assertion and prefers `FindCtx`
when available. This keeps existing in-process callers untouched
while leaving the door open for v0.3.0 network resolvers.

## Shared primitives

```
StripFrontmatter ──┐
                   ├── used by all resolvers + Embedded
ParseFrontmatter ──┘

ParseMetadata ──> Metadata struct
                   ├── typed slots for spec fields
                   └── Extra map for non-standard keys

ValidateName ──> spec compliance check (called by Embedded
                  at construction; consumers may call manually)
```

Frontmatter parser is intentionally minimal:
- YAML: top-level scalars, quoted strings, flow lists, comments,
  `metadata:` block top-level keys flattened into `Extra`. No nested
  maps, no anchors, no multi-doc.
- JSON: standard `encoding/json` unmarshal into a typed helper struct.

A `bufio.Scanner` with a 1 MiB buffer handles streaming input.
Default 64 KiB would silently truncate large skills.

## Hot-reload semantics

`Embedded.Body()` and (with `WithMtimeCache`) `DirTier.Find` both
mtime-cache by file path. Per call:

1. `os.Stat(path)` — microseconds
2. Compare `stat.ModTime().UnixNano()` against cached value
3. Same → return cached body
4. Different → `os.ReadFile` + `StripFrontmatter`, update cache,
   return new body

mtime polling is chosen over fsnotify because:

- fsnotify (inotify) does not fire on NFS, SMB, or several Docker
  bind-mount configurations. SSHFS mounts in our environment fall
  in this gap.
- The cost of mtime polling is one syscall per `Body()` call —
  negligible vs the LLM call that follows.
- Operator mental model is simpler: "edit the file, next request
  picks it up." No daemon-side state.

## Concurrency

- `Embedded` is safe for concurrent use. The per-path mutex covers
  stat+read+cache-update so concurrent callers see a consistent
  body during a file rewrite.
- `Catalog` and tier types are read-mostly after construction. Their
  internal maps are populated at construction (`EmbedFSTier`,
  `PluginTier`) or via mutex-protected reads (`DirTier` with
  `WithMtimeCache`).
- `ContextResolver` implementations are responsible for their own
  cancellation handling.

## Security

- **Path traversal in `DirTier`.** `os.Lstat` skips symlink entries.
  After resolving the SKILL.md path with `filepath.EvalSymlinks`, we
  verify `filepath.Rel(dir, resolved)` does not start with `..`. A
  user-planted symlink `evil-skill -> /etc/passwd.md` is silently
  skipped + logged at slog Warn.
- **DoS via huge env override file.** `Embedded.Body()` rejects env
  files larger than 1 MiB at stat time, before reading.
- **NUL bytes in skill body.** `StripFrontmatter` removes them so
  bufio.Scanner does not silently truncate.
- **Malformed frontmatter.** Never fatal. JSON parse failure → fall
  through to YAML; YAML format failures land unknown keys in `Extra`.
  Caller always gets a body (possibly without metadata).

## Spec conformance

Required frontmatter fields per agentskills.io:

| Field | Spec | Our struct |
|-------|------|-----------|
| `name` | required, 1–64 lowercase-alpha+hyphens, must match dir name | `Metadata.Name` + `ValidateName` |
| `description` | required, ≤1024 chars | `Metadata.Description` |
| `license` | optional | `Metadata.License` |
| `compatibility` | optional, ≤500 chars | `Metadata.Compatibility` |
| `metadata` | optional arbitrary map | flattened into `Metadata.Extra` |
| `allowed-tools` | optional, space-sep string OR YAML list | `Metadata.AllowedTools []string` |

Claude Code extensions:

| Field | Spec | Our struct |
|-------|------|-----------|
| `when_to_use` | discovery rule context | `Metadata.WhenToUse` |
| `disable-model-invocation` | bool | `Metadata.DisableModelInvocation` |
| `user-invocable` | bool | `Metadata.UserInvocable *bool` |
| `model`, `effort`, `context`, `argument-hint`, `arguments` | runtime hints | `Metadata.Extra` |

agentskills-go custom fields (widely useful, not in spec):

| Field | Purpose |
|-------|---------|
| `version` | semver string for migration tracking |
| `locale` | language tag (en/ru/zh); routing is consumer concern in v0.1 |
| `tags` | flow list or comma-separated for categorization |

## Module layout choice

Single root package (`package skill`). Reasons:

- Consumers type `skill.NewEmbedded(...)` and `skill.Catalog{}` —
  short and ergonomic.
- The `-go` suffix in the repo name is for ecosystem discovery on
  agentskills.io; not part of the import alias.
- A future MCP / Langfuse / HTTP resolver lives in its own
  sub-package (e.g. `mcpresolver/`) with its own go.mod so the core
  package stays stdlib-only.

## Versioning policy

- Pre-1.0: minor releases may break public API; document changes in
  CHANGELOG.
- 1.0.0+: strict semver. Breaking changes only in major bumps. Two
  minor releases between deprecation marker and removal.
- Spec version tracking: agentskills.io spec evolves; we pin
  conformance to the spec version at the time of each release in
  the CHANGELOG entry.

## Testing strategy

- Each public function has direct unit tests with table-driven
  fixtures.
- Each `Resolver` has tier_testdata fixtures on disk.
- `-race` clean is a CI gate.
- ≥90% line coverage is a CI gate.
- A future v0.2.x will add `cmd/skillvalidate` plus golden-file tests
  using real skill files from the wider ecosystem (Anthropic skills,
  superpowers plugin) for regression detection on spec changes.
