package skillkit

import (
	"context"
	"log/slog"
	"sort"
	"strings"
)

// Catalog aggregates multiple Tiers into a unified skill registry.
// Tiers are searched in order; the first match wins. Duplicate skill
// names across tiers are deduplicated so the higher-priority tier wins.
type Catalog struct {
	tiers []Tier
}

// NewCatalog creates a Catalog from the given tiers, searched in order.
func NewCatalog(tiers ...Tier) *Catalog {
	return &Catalog{tiers: tiers}
}

// List returns all unique skills across all tiers, sorted by Name.
// When the same name appears in multiple tiers the first tier wins.
func (c *Catalog) List() []SkillInfo {
	seen := make(map[string]bool)
	var result []SkillInfo
	for _, t := range c.tiers {
		t.Resolver.Walk(func(info SkillInfo) {
			if seen[info.Name] {
				return
			}
			seen[info.Name] = true
			info = c.tagSource(t, info)
			result = append(result, info)
		})
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})
	return result
}

// Load searches tiers in order and returns the first match for name.
func (c *Catalog) Load(name string) (body string, info SkillInfo, ok bool) {
	for _, t := range c.tiers {
		si, b, found := t.Resolver.Find(name)
		if !found {
			continue
		}
		si = c.tagSource(t, si)
		return b, si, true
	}
	return "", SkillInfo{}, false
}

// LoadCtx searches tiers in order using context-aware lookup when the
// tier implements ContextResolver. Falls back to Find otherwise.
func (c *Catalog) LoadCtx(ctx context.Context, name string) (body string, info SkillInfo, ok bool, err error) {
	for _, t := range c.tiers {
		if cr, isCtx := t.Resolver.(ContextResolver); isCtx {
			si, b, found, findErr := cr.FindCtx(ctx, name)
			if findErr != nil {
				return "", SkillInfo{}, false, findErr
			}
			if !found {
				continue
			}
			si = c.tagSource(t, si)
			return b, si, true, nil
		}
		si, b, found := t.Resolver.Find(name)
		if !found {
			continue
		}
		si = c.tagSource(t, si)
		return b, si, true, nil
	}
	return "", SkillInfo{}, false, nil
}

// LoadMany returns a concatenated system-prompt block for the given names.
// Each skill is rendered as "### Skill: <name>\n\n<body>\n\n---\n\n".
// The trailing separator is stripped. Missing skills are logged at Debug
// and silently skipped. Returns "" for an empty names slice.
func (c *Catalog) LoadMany(names []string) string {
	if len(names) == 0 {
		return ""
	}
	var parts []string
	for _, name := range names {
		body, _, ok := c.Load(name)
		if !ok {
			slog.Debug("skillkit.Catalog.LoadMany: skill not found, skipping", "name", name)
			continue
		}
		parts = append(parts, "### Skill: "+name+"\n\n"+body)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// LoadManyCtx is the context-aware variant of LoadMany.
func (c *Catalog) LoadManyCtx(ctx context.Context, names []string) string {
	if len(names) == 0 {
		return ""
	}
	var parts []string
	for _, name := range names {
		body, _, ok, err := c.LoadCtx(ctx, name)
		if err != nil {
			slog.Debug("skillkit.Catalog.LoadManyCtx: skill load error, skipping",
				"name", name, "err", err)
			continue
		}
		if !ok {
			slog.Debug("skillkit.Catalog.LoadManyCtx: skill not found, skipping", "name", name)
			continue
		}
		parts = append(parts, "### Skill: "+name+"\n\n"+body)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n---\n\n")
}

// tagSource sets info.Source = tier.Name for non-plugin resolvers.
// PluginTier already populates Source as "<tier>:<plugin>" (detected
// by the presence of ":"), so those are returned unchanged.
func (c *Catalog) tagSource(t Tier, info SkillInfo) SkillInfo {
	if strings.Contains(info.Source, ":") {
		// PluginTier already set source.
		return info
	}
	info.Source = t.Name
	return info
}
