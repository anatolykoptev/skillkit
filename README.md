# agentskills-go

Go loader for the [Agent Skills open standard](https://agentskills.io)
(`SKILL.md` + YAML frontmatter), compatible with the format adopted by
Claude Code, Cursor, GitHub Copilot, VS Code, Gemini CLI, OpenAI Codex,
JetBrains Junie, OpenHands, Goose, and 30+ other agentic tools.

> **Status:** in development. First reference Go implementation of the
> agentskills.io standard. See `docs/plans/` for the implementation plan.

## What it does

Two independent APIs in one zero-dependency package:

- **`skill.Embedded`** — single skill baked into a binary via `//go:embed`,
  with optional env-path override for hot-reload during prompt iteration.
  Designed for services that ship one skill per binary (e.g. an answer
  extractor in a search pipeline).

- **`skill.Catalog`** — multi-skill discovery across tiered sources
  (filesystem directories, embed.FS, plugin entries). Designed for agent
  CLIs that load many skills and inject summaries into a system prompt.

Both APIs share the same frontmatter primitives (`StripFrontmatter`,
`ParseFrontmatter`, `ParseMetadata`).

## Why

Until now, every Go service that loaded skills hand-rolled its own loader:
four near-identical implementations across MemDB, dozor, and vaelor; a
fifth in pre-release form on a feature branch; three different YAML
frontmatter parsers between them. Each drifted from the others over time
(MemDB shipped a Go const + `.md` file with diverging text in production).

The Agent Skills open standard published in late 2025 turned this from a
duplication problem into a portability problem: a skill written for one
agent should work in any other without modification. This package
implements the spec strictly so that skills authored against
`agentskills-go` run unchanged in Cursor, Claude Code, Junie, and the
rest of the ecosystem.

## Install

```bash
go get github.com/anatolykoptev/agentskills-go
```

Requires Go 1.26+. Zero non-stdlib runtime dependencies.

## Quickstart — Pattern A (Embedded)

```go
package myservice

import (
    _ "embed"
    skill "github.com/anatolykoptev/agentskills-go"
)

//go:embed skills/answer-extractor/SKILL.md
var rawSkill string

var answerExtractor = skill.NewEmbedded(
    "answer-extractor",
    "MYSERVICE_SKILL_PATH", // env override path for hot-reload (optional)
    rawSkill,
)

func systemPrompt() string {
    return answerExtractor.Body()
}
```

## Quickstart — Pattern B (Catalog)

```go
package mycli

import (
    skill "github.com/anatolykoptev/agentskills-go"
)

func newCatalog(workspaceDir, builtinDir string) *skill.Catalog {
    return skill.NewCatalog(
        skill.NewDirTier("workspace", workspaceDir),
        skill.NewDirTier("builtin",   builtinDir),
    )
}

func loadDocReview(c *skill.Catalog) (string, bool) {
    body, _, ok := c.Load("doc-review")
    return body, ok
}

func systemPromptSummary(c *skill.Catalog) string {
    return c.BuildSummary(skill.SummaryXML)
}
```

## Status

Not yet released. See `docs/plans/2026-04-30-init.md` for the work plan.

## License

MIT — see [LICENSE](LICENSE).

## Related

- [agentskills.io](https://agentskills.io) — the open standard this package implements
- [agentskills/agentskills](https://github.com/agentskills/agentskills) — spec repo + `skills-ref` CLI validator
- [Anthropic Agent Skills docs](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview) — Claude Code extensions
