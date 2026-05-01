# skillkit — Usage Guide

This document covers both patterns exposed by the package with
real-world examples, a frontmatter reference, name validation rules,
hot-reload mechanics, migration notes, and a spec conformance table.

---

## 1. Why this package

Every Go service that loads skills has historically hand-rolled its own
loader: a `//go:embed` glob, an env-override path, an mtime check, a
bespoke YAML parser. None of these agree on frontmatter field names, name
validation rules, or resolution order — so a skill that works in one
service silently breaks in another.

The agentskills.io open standard fixes this at the ecosystem level. A
skill is a directory (`skill-name/SKILL.md`) with YAML or JSON
frontmatter; 35+ agentic tools — Claude Code, Cursor, GitHub Copilot,
JetBrains Junie, Gemini CLI, OpenAI Codex, and others — agree on the
same field names and directory layout. `skillkit` implements that
standard strictly so that skills authored against it run unchanged in any
conformant agent. It also eliminates ~150–250 lines of boilerplate per
service.

---

## 2. Pattern A: Embedded single skill

Use Pattern A when a binary ships exactly one skill baked in via
`//go:embed`. An optional env variable lets operators point to a
filesystem file for hot-reload during prompt iteration, without restarting
the service.

```go
package myservice

import (
    _ "embed"
    "log/slog"

    "github.com/anatolykoptev/skillkit"
)

//go:embed skills/d10-extractor/SKILL.md
var rawSkill string

// NewEmbedded panics at startup if name is invalid or body is empty after
// frontmatter strip — catching configuration errors at binary init time.
var skill = skillkit.NewEmbedded(
    "d10-extractor",          // skill name (must match dir name per spec)
    "MEMDB_D10_SKILL_PATH",   // env var: set to a file path to override
    rawSkill,
)

func init() {
    slog.Info("skill resolution", "state", skill.Diagnostic())
    // Prints one of:
    //   "embedded default"
    //   "env override /etc/myservice/d10-extractor.md"
    //   "env override /tmp/edit.md UNREADABLE → embedded default"
}

func buildPrompt() string {
    return skill.Body() // always non-empty; hot-reloads from env path if set
}
```

`skill.Metadata()` returns the parsed frontmatter from the embedded file
(not from any env-override file — metadata is stable across hot-reloads).

`skill.Diagnostic()` re-stats the env path on every call; use it in
startup logs or a health endpoint, not in the hot path.

---

## 3. Pattern B: Multi-tier catalog (workspace + builtin)

Use Pattern B when an agent CLI loads many skills from the filesystem.
Tiers are searched in priority order; the first match wins.

```go
package mycli

import (
    "log/slog"

    "github.com/anatolykoptev/skillkit"
)

func newCatalog(workspaceDir, builtinDir string) *skillkit.Catalog {
    return skillkit.NewCatalog(
        skillkit.NewDirTier("workspace", workspaceDir), // user-editable, wins
        skillkit.NewDirTier("builtin",   builtinDir),   // shipped with agent
    )
}

func loadDocReview(cat *skillkit.Catalog) {
    body, info, ok := cat.Load("doc-review")
    if !ok {
        slog.Warn("skill not found", "name", "doc-review")
        return
    }
    _ = body        // skill body (frontmatter stripped)
    _ = info.Source // "workspace" or "builtin" — which tier matched
    _ = info.Dir    // absolute path to skill dir; use to resolve siblings
}

func loadMany(cat *skillkit.Catalog) string {
    // Returns a single string ready for system-prompt injection.
    // Format per skill: "### Skill: <name>\n\n<body>", joined with "\n\n---\n\n".
    return cat.LoadMany([]string{"doc-review", "code-summary", "translate"})
}

func systemPromptSkillBlock(cat *skillkit.Catalog) string {
    // Compact XML summary of all known skills: name, path, source, description.
    // Suitable for injection at the top of a system prompt.
    return cat.BuildSummary(skillkit.SummaryXML)
}
```

`DirTier` expects the layout `<dir>/<skill-name>/SKILL.md`. One level of
subdirectories only — no recursive scan. Symlinks to skill directories are
skipped with a `slog.Warn`.

---

## 4. Pattern B: Plugin-namespaced catalog

When a plugin system contributes skills alongside built-in ones, use
`NewPluginTier`. The tier maintains two indices: bare name and
`plugin:skill` namespaced name.

```go
package mycli

import (
    "github.com/anatolykoptev/skillkit"
)

func newPluginCatalog(workspaceDir string, pluginSkills []skillkit.PluginEntry) *skillkit.Catalog {
    return skillkit.NewCatalog(
        skillkit.NewDirTier("workspace", workspaceDir),
        skillkit.NewPluginTier("plugin", pluginSkills),
    )
}

// Example entries populated by the plugin discovery layer:
var entries = []skillkit.PluginEntry{
    {
        PluginName: "code-review",
        SkillName:  "diff-summary",
        Body:       rawDiffSummarySkill, // pre-loaded body; no disk read at runtime
    },
    {
        PluginName: "code-review",
        SkillName:  "commit-message",
        Path:       "/usr/local/share/plugins/code-review/commit-message/SKILL.md",
    },
}

func lookups(cat *skillkit.Catalog) {
    // Bare-name lookup: returns the first plugin entry with SkillName "diff-summary".
    body, info, ok := cat.Load("diff-summary")
    _ = body
    _ = ok
    _ = info.Source // "plugin:code-review"  ← tier:plugin format

    // Namespaced lookup: explicit plugin + skill.
    body2, _, ok2 := cat.Load("code-review:diff-summary")
    _ = body2
    _ = ok2
}
```

`SkillInfo.Source` is `"<tier>:<plugin>"` for plugin tiers (e.g.
`"plugin:code-review"`) and just `"<tier>"` for DirTier / EmbedFSTier
(e.g. `"workspace"`). The Catalog tags this automatically — resolvers that
already set a colon-containing Source are left unchanged.

When multiple entries share the same bare `SkillName`, the first entry
wins and a `slog.Warn` is emitted for the conflict.

---

## 5. Pattern B: embed.FS multi-skill catalog

Use `NewEmbedFSTier` to ship a curated set of skills inside the binary
with no operator setup required.

```go
package myagent

import (
    "embed"

    "github.com/anatolykoptev/skillkit"
)

// The glob must match <root>/<skill-name>/SKILL.md exactly.
// All matched files are parsed once at binary init (NewEmbedFSTier call).

//go:embed skills/*/SKILL.md
var embeddedSkills embed.FS

var cat = skillkit.NewCatalog(
    skillkit.NewEmbedFSTier("builtin", embeddedSkills, "skills"),
)

func listBuiltin() []skillkit.SkillInfo {
    return cat.List() // sorted by name, deduplicated across tiers
}
```

`EmbedFSTier` uses forward-slash paths internally (`embed.FS` always does)
regardless of the host OS. `SkillInfo.Dir` is set to the forward-slash
path of the skill directory inside the FS — callers that resolve sibling
files must use `embed.FS.ReadFile`, not `os.Open`.

---

## 6. Frontmatter reference

skillkit parses two formats. Both require a blank line between the
frontmatter block and the body.

### YAML (Markdown standard — recommended)

```markdown
---
name: d10-extractor
description: Extracts the ten most important atomic facts from a document.
license: MIT
compatibility: Claude 3.5 Sonnet+
when_to_use: Use when summarizing dense technical documents.
allowed-tools: [Read, Bash]
disable-model-invocation: false
user-invocable: true
version: 1.0.0
locale: en
tags: [extraction, summarization]
metadata:
  effort: low
  context: 8192
---

# D10 Extractor

Given a document, return the ten most important atomic facts...
```

`allowed-tools` accepts either a YAML flow list (`[Read, Bash]`) or a
space-separated string (`Read Bash`). `tags` accepts either a YAML flow
list or a comma-separated string (`extraction, summarization`).

### JSON

```markdown
{
  "name": "d10-extractor",
  "description": "Extracts the ten most important atomic facts from a document.",
  "license": "MIT",
  "when_to_use": "Use when summarizing dense technical documents.",
  "allowed-tools": ["Read", "Bash"],
  "version": "1.0.0",
  "tags": ["extraction", "summarization"],
  "metadata": {
    "effort": "low"
  }
}

# D10 Extractor

Given a document, return the ten most important atomic facts...
```

JSON frontmatter: top-level object on the first line(s), then exactly one
blank line, then the body. Trailing non-whitespace on the closing-brace
line causes the frontmatter to be silently ignored (body returned as-is).

---

## 7. Metadata fields reference

All fields correspond to frontmatter keys. Unrecognized top-level keys
land in `Extra`.

| Struct field | Frontmatter key | Type | Notes |
|---|---|---|---|
| `Name` | `name` | `string` | Required; validated by `ValidateName` |
| `Description` | `description` | `string` | Required; warn if > 1024 chars |
| `License` | `license` | `string` | SPDX identifier recommended |
| `Compatibility` | `compatibility` | `string` | Model version constraint, free-form |
| `WhenToUse` | `when_to_use` | `string` | Claude Code discovery hint |
| `AllowedTools` | `allowed-tools` | `[]string` | Flow list or space-separated |
| `DisableModelInvocation` | `disable-model-invocation` | `bool` | Claude Code: skip LLM call |
| `UserInvocable` | `user-invocable` | `*bool` | `nil` = default (true); 3-state |
| `Version` | `version` | `string` | Semver for migration tracking |
| `Locale` | `locale` | `string` | BCP 47 tag (en, ru, zh-CN) |
| `Tags` | `tags` | `[]string` | Flow list or comma-separated |
| `Extra` | top-level unknown keys + `metadata:` sub-keys | `map[string]string` | Catch-all for non-standard fields |

The `metadata:` YAML block flattens its top-level keys into `Extra`. For
example, `metadata:\n  effort: low` adds `Extra["effort"] = "low"`.
JSON uses the `"metadata"` key for the same map.

---

## 8. Multi-file skills via SkillInfo.Dir

The agentskills.io spec defines a standard layout for skills with
supplementary content:

```
skill-name/
  SKILL.md          ← frontmatter + body
  scripts/          ← runnable helpers (execution is caller's concern)
  references/       ← long-form context documents
  assets/           ← images, data files
```

`SkillInfo.Dir` is the absolute path to the skill directory. Use it to
resolve sibling files:

```go
body, info, ok := cat.Load("d10")
if !ok {
    return
}

// Load a sibling reference document.
refPath := filepath.Join(info.Dir, "references", "edge_cases.md")
refContent, err := os.ReadFile(refPath)
if err != nil {
    // Reference is optional; handle gracefully.
    _ = err
}
```

`PluginEntry` entries with only `Body` set (no `Path`) have an empty
`Dir`. Always check `info.Dir != ""` before calling `filepath.Join` on it.

For `EmbedFSTier`, `Dir` is a forward-slash path into the `embed.FS`. Use
`embed.FS.ReadFile(filepath.ToSlash(filepath.Join(info.Dir, "references", "edge_cases.md")))`
or construct the path with `path.Join` (not `filepath.Join`).

---

## 9. Name validation rules (agentskills.io spec)

The spec mandates:

- 1–64 characters
- Lowercase ASCII alphanumeric and hyphens only
- No leading hyphen
- No trailing hyphen
- No consecutive hyphens (`--`)
- On-disk directory name must match the skill's `name` field

`ValidateName(name, dirName string) error` enforces all rules. Pass `""`
as `dirName` to skip the directory-match check (used by `Embedded`).

```go
// Valid names:
// "d10-extractor"   ✓
// "doc-review"      ✓
// "translate"       ✓
// "code-review-v2"  ✓

// Invalid names (ValidateName returns a non-nil error):
// ""                — empty
// "D10-Extractor"   — uppercase
// "-extractor"      — leading hyphen
// "extractor-"      — trailing hyphen
// "code--review"    — consecutive hyphens
// strings.Repeat("a", 65)  — exceeds 64 chars
```

`NewEmbedded` calls `ValidateName(name, "")` and panics on failure — the
error surfaces at binary startup, not at request time. `DirTier` uses the
directory name as the skill name without re-validating it (the spec check
is at authoring time, enforced by `skills-ref` in CI).

---

## 10. Hot-reload mechanics

`Embedded.Body()` and `DirTier.Find` (with `WithMtimeCache`) share the
same mtime-polling strategy:

1. `os.Stat(path)` — one syscall, microseconds.
2. Compare `stat.ModTime().UnixNano()` against the cached value.
3. Same → return cached body (no disk read).
4. Different → `os.ReadFile` + `StripFrontmatter`, update cache, return
   new body.

The net effect: edit a skill file, next request picks it up. No restarts.

### Embedded-specific behavior

- Body is always non-empty. If the env-override file is transiently
  unreadable (e.g. during an atomic rename where a `.tmp` file is moved
  over the target), `Body()` returns the last-known-good cached body
  rather than falling back to the embedded default. This avoids a brief
  blip back to the old default during a live file update.
- Files larger than 1 MiB are rejected at stat time (DoS guard). The size
  limit applies only to env-override files; the embedded default has no
  cap.

### Why not fsnotify

inotify-style watchers do not fire on NFS, SMB, or several Docker
bind-mount configurations. SSHFS mounts fall in this gap. The cost of one
`os.Stat` per `Body()` call is negligible next to the LLM round-trip that
follows. mtime polling is the right primitive for the deployment topology
and will not change (see `docs/ROADMAP.md` anti-roadmap).

---

## 11. Migration notes from hand-rolled loaders

Existing skill files (`SKILL.md` with YAML or JSON frontmatter) work
unchanged — skillkit is spec-compliant and parses the same frontmatter
fields in the same way.

### MemDB `skill_loader.go` → `skillkit.NewEmbedded`

Before (~150 lines):
- `//go:embed …` + `strings.TrimSpace(strings.SplitN(raw, "---", 3)[2])`
- Manual env var read + `os.Stat` + `os.ReadFile` + mtime comparison
- Separate `Metadata` struct with partial field coverage

After (3 lines):
```go
//go:embed skills/d10-extractor/SKILL.md
var rawSkill string

var skill = skillkit.NewEmbedded("d10-extractor", "MEMDB_D10_SKILL_PATH", rawSkill)
```

### dozor `internal/skills/Loader` → `skillkit.NewCatalog` + `NewDirTier`

Before (~200 lines):
- `os.ReadDir` + per-entry `os.ReadFile` + custom YAML parser
- Separate workspace and builtin resolution paths
- No mtime caching

After (5 lines):
```go
cat := skillkit.NewCatalog(
    skillkit.NewDirTier("workspace", workspaceDir, skillkit.WithMtimeCache()),
    skillkit.NewDirTier("builtin",   builtinDir),
)
```

### vaelor `pkg/skills/SkillsLoader` → `Catalog` + DirTier × N + PluginTier

Before (~250 lines):
- Three-level directory scan (workspace / global / builtin)
- Plugin discovery loop with bare + namespaced name indices maintained
  manually
- Inline summary rendering for system prompts

After (8 lines):
```go
cat := skillkit.NewCatalog(
    skillkit.NewDirTier("workspace", workspaceDir),
    skillkit.NewDirTier("global",    globalDir),
    skillkit.NewDirTier("builtin",   builtinDir),
    skillkit.NewPluginTier("plugin", discoveredEntries),
)
summary := cat.BuildSummary(skillkit.SummaryXML)
```

---

## 12. Spec conformance table

| Spec field | Required? | skillkit slot |
|---|---|---|
| `name` | required | `Metadata.Name`; `ValidateName` enforces all spec rules |
| `description` | required | `Metadata.Description`; warn if > 1024 chars |
| `license` | optional | `Metadata.License` |
| `compatibility` | optional | `Metadata.Compatibility` |
| `metadata` | optional map | keys flattened into `Metadata.Extra` |
| `allowed-tools` | optional | `Metadata.AllowedTools []string`; parses flow list and space-separated |
| `when_to_use` (Claude Code ext.) | optional | `Metadata.WhenToUse` |
| `disable-model-invocation` (Claude Code ext.) | optional | `Metadata.DisableModelInvocation bool` |
| `user-invocable` (Claude Code ext.) | optional | `Metadata.UserInvocable *bool` (nil = unset, true = default) |

Runtime hint fields from the Claude Code spec (`model`, `effort`,
`context`, `argument-hint`, `arguments`) are not typed slots — they land
in `Metadata.Extra` and are available to consumers without requiring a
skillkit update for each new extension.

The `skills-ref` CLI from `github.com/agentskills/agentskills` validates
spec conformance independently. Running it against your `SKILL.md` files
in CI is recommended before publishing skills for use with other agents.
