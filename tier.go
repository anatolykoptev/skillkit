package skillkit

import (
	"context"
	"embed"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

// SkillInfo describes a discovered skill.
type SkillInfo struct {
	Name   string
	Path   string // resolver-defined; "" for plugin sources
	Dir    string // skill directory (SKILL.md's parent); populated by all tiers
	Source string // tier name; "<tier>:<plugin>" for plugin tier
	Metadata
}

// Resolver is the source-agnostic lookup interface.
type Resolver interface {
	Find(name string) (info SkillInfo, body string, ok bool)
	Walk(yield func(SkillInfo))
}

// ContextResolver extends Resolver for network-backed sources.
type ContextResolver interface {
	Resolver
	FindCtx(ctx context.Context, name string) (SkillInfo, string, bool, error)
}

// Tier pairs a human-readable name with a Resolver.
// Source tagging happens at the Catalog layer (Task D); the Resolver
// itself leaves SkillInfo.Source empty.
type Tier struct {
	Name     string
	Resolver Resolver
}

// ---------------------------------------------------------------------------
// DirTier
// ---------------------------------------------------------------------------

type dirTier struct {
	name          string
	dir           string
	skillFilename string
	useMtimeCache bool

	mu    sync.Mutex
	cache map[string]dirCacheEntry
}

type dirCacheEntry struct {
	mtime int64 // UnixNano
	body  string
	info  SkillInfo
}

// DirTierOption configures a DirTier.
type DirTierOption func(*dirTier)

// WithSkillFilename overrides the default "SKILL.md" filename.
func WithSkillFilename(filename string) DirTierOption {
	return func(d *dirTier) {
		d.skillFilename = filename
	}
}

// WithMtimeCache enables an opt-in mtime-based body cache.
func WithMtimeCache() DirTierOption {
	return func(d *dirTier) {
		d.useMtimeCache = true
	}
}

// NewDirTier creates a filesystem tier that scans one level deep inside dir.
// Each sub-directory that contains a SKILL.md (or the configured filename)
// is treated as a skill. Symlinks are skipped with a slog.Warn.
func NewDirTier(name, dir string, opts ...DirTierOption) Tier {
	d := &dirTier{
		name:          name,
		dir:           dir,
		skillFilename: "SKILL.md",
	}
	for _, o := range opts {
		o(d)
	}
	return Tier{Name: name, Resolver: d}
}

func (d *dirTier) Find(name string) (SkillInfo, string, bool) {
	skillDir := filepath.Join(d.dir, name)
	skillFile := filepath.Join(skillDir, d.skillFilename)

	// Guard: the skill sub-directory itself must not be a symlink.
	lfi, err := os.Lstat(skillDir)
	if err != nil {
		return SkillInfo{}, "", false
	}
	if lfi.Mode()&os.ModeSymlink != 0 {
		slog.Warn("skillkit.DirTier: skill directory is a symlink, skipping",
			"tier", d.name, "path", skillDir)
		return SkillInfo{}, "", false
	}

	info, body, ok := d.loadFile(skillFile)
	if !ok {
		return SkillInfo{}, "", false
	}
	return info, body, true
}

// loadFile reads, validates, and (optionally) caches the contents of skillFile.
func (d *dirTier) loadFile(skillFile string) (SkillInfo, string, bool) {
	// Symlink traversal guard on the SKILL.md itself.
	lfi, err := os.Lstat(skillFile)
	if err != nil {
		return SkillInfo{}, "", false
	}
	if lfi.Mode()&os.ModeSymlink != 0 {
		resolved, resolveErr := filepath.EvalSymlinks(skillFile)
		if resolveErr != nil {
			slog.Warn("skillkit.DirTier: cannot resolve symlink, skipping",
				"path", skillFile, "err", resolveErr)
			return SkillInfo{}, "", false
		}
		rel, relErr := filepath.Rel(d.dir, resolved)
		if relErr != nil || strings.HasPrefix(rel, "..") {
			slog.Warn("skillkit.DirTier: symlink escapes tier root, skipping",
				"path", skillFile, "resolved", resolved)
			return SkillInfo{}, "", false
		}
	}

	if d.useMtimeCache {
		if info, body, hit := d.cacheGet(skillFile); hit {
			return info, body, true
		}
	}

	raw, err := os.ReadFile(skillFile) //nolint:gosec // G304: path constructed from tier root
	if err != nil {
		return SkillInfo{}, "", false
	}

	body := StripFrontmatter(string(raw))
	if body == "" {
		slog.Warn("skillkit.DirTier: skill has empty body after frontmatter strip, skipping",
			"path", skillFile)
		return SkillInfo{}, "", false
	}

	skillDir := filepath.Dir(skillFile)
	skillName := filepath.Base(skillDir)
	meta := ParseMetadata(string(raw))

	si := SkillInfo{
		Name:     skillName,
		Path:     skillFile,
		Dir:      skillDir,
		Metadata: meta,
	}

	if d.useMtimeCache {
		fi, statErr := os.Stat(skillFile) //nolint:gosec // G304
		if statErr == nil {
			d.cachePut(skillFile, fi.ModTime().UnixNano(), body, si)
		}
	}

	return si, body, true
}

// cacheGet returns (info, body, true) when the cache has a fresh entry.
func (d *dirTier) cacheGet(skillFile string) (SkillInfo, string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cache == nil {
		return SkillInfo{}, "", false
	}
	entry, ok := d.cache[skillFile]
	if !ok {
		return SkillInfo{}, "", false
	}

	fi, err := os.Stat(skillFile) //nolint:gosec // G304
	if err != nil || fi.ModTime().UnixNano() != entry.mtime {
		return SkillInfo{}, "", false
	}
	return entry.info, entry.body, true
}

// cachePut stores or replaces a cache entry.
func (d *dirTier) cachePut(skillFile string, mtime int64, body string, info SkillInfo) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.cache == nil {
		d.cache = make(map[string]dirCacheEntry)
	}
	d.cache[skillFile] = dirCacheEntry{mtime: mtime, body: body, info: info}
}

func (d *dirTier) Walk(yield func(SkillInfo)) {
	entries, err := os.ReadDir(d.dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			slog.Warn("skillkit.DirTier: skill directory is a symlink, skipping",
				"tier", d.name, "name", e.Name())
			continue
		}
		skillFile := filepath.Join(d.dir, e.Name(), d.skillFilename)
		si, _, ok := d.loadFile(skillFile)
		if !ok {
			continue
		}
		yield(si)
	}
}

// ---------------------------------------------------------------------------
// EmbedFSTier
// ---------------------------------------------------------------------------

type embedFSTier struct {
	name   string
	skills map[string]embedEntry // keyed by skill name
}

type embedEntry struct {
	info SkillInfo
	body string
}

// embedFSConfig holds construction-time configuration for an EmbedFSTier.
type embedFSConfig struct {
	skillFilename string
}

// EmbedFSOption configures an EmbedFSTier.
type EmbedFSOption func(*embedFSConfig)

// WithEmbedSkillFilename overrides the default "SKILL.md" filename.
func WithEmbedSkillFilename(filename string) EmbedFSOption {
	return func(cfg *embedFSConfig) {
		cfg.skillFilename = filename
	}
}

// NewEmbedFSTier creates a tier that reads skills from an embed.FS.
// All skill files are parsed once at construction. root is the prefix
// directory inside the embed.FS (e.g. "tier_testdata/embedfs").
//
// Dir uses forward-slash path.Join (not filepath.Join) because embed.FS
// always uses forward slashes.
func NewEmbedFSTier(name string, fsys embed.FS, root string, opts ...EmbedFSOption) Tier {
	cfg := embedFSConfig{skillFilename: "SKILL.md"}
	for _, o := range opts {
		o(&cfg)
	}
	e := &embedFSTier{
		name:   name,
		skills: make(map[string]embedEntry),
	}
	buildEmbedFS(e, fsys, root, cfg.skillFilename)
	return Tier{Name: name, Resolver: e}
}

// buildEmbedFS walks the embed.FS and populates the tier's skills map.
func buildEmbedFS(e *embedFSTier, fsys embed.FS, root, skillFilename string) {
	_ = fs.WalkDir(fsys, root, func(fpath string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			return nil
		}
		// Match only <root>/<skillName>/<skillFilename>.
		rel := strings.TrimPrefix(fpath, root+"/")
		parts := strings.SplitN(rel, "/", 2) //nolint:mnd // 2 parts: skillName/filename
		if len(parts) != 2 || parts[1] != skillFilename {
			return nil
		}
		skillName := parts[0]

		raw, readErr := fsys.ReadFile(fpath)
		if readErr != nil {
			return nil
		}

		body := StripFrontmatter(string(raw))
		if body == "" {
			slog.Warn("skillkit.EmbedFSTier: skill has empty body, skipping",
				"path", fpath)
			return nil
		}

		meta := ParseMetadata(string(raw))
		si := SkillInfo{
			Name:     skillName,
			Path:     fpath,
			Dir:      path.Join(root, skillName),
			Metadata: meta,
		}
		e.skills[skillName] = embedEntry{info: si, body: body}
		return nil
	})
}

func (e *embedFSTier) Find(name string) (SkillInfo, string, bool) {
	entry, ok := e.skills[name]
	if !ok {
		return SkillInfo{}, "", false
	}
	return entry.info, entry.body, true
}

func (e *embedFSTier) Walk(yield func(SkillInfo)) {
	for _, entry := range e.skills {
		yield(entry.info)
	}
}

// ---------------------------------------------------------------------------
// PluginTier
// ---------------------------------------------------------------------------

// PluginEntry describes a single skill provided by a plugin.
type PluginEntry struct {
	PluginName string
	SkillName  string
	Path       string // optional: path to skill file
	Body       string // optional: pre-loaded body (preferred over Path)
}

type pluginTier struct {
	name    string
	entries map[string]PluginEntry // keyed by "pluginName:skillName"
	bare    map[string]string      // bare name → "pluginName:skillName"
}

// NewPluginTier creates a tier from a slice of pre-resolved entries.
// Both bare ("foo") and namespaced ("plugin:foo") lookups are supported.
// When multiple entries share the same (PluginName, SkillName), the first
// wins and a slog.Warn is emitted listing duplicates.
func NewPluginTier(name string, entries []PluginEntry) Tier {
	p := &pluginTier{
		name:    name,
		entries: make(map[string]PluginEntry, len(entries)),
		bare:    make(map[string]string, len(entries)),
	}

	type dupKey struct{ plugin, skill string }
	seen := make(map[dupKey]bool)

	for _, e := range entries {
		key := e.PluginName + ":" + e.SkillName
		dk := dupKey{e.PluginName, e.SkillName}
		if seen[dk] {
			slog.Warn("skillkit.PluginTier: duplicate (PluginName, SkillName) entry, skipping",
				"tier", name, "plugin", e.PluginName, "skill", e.SkillName)
			continue
		}
		seen[dk] = true
		p.entries[key] = e

		// First entry for a bare name wins.
		if _, conflict := p.bare[e.SkillName]; !conflict {
			p.bare[e.SkillName] = key
		} else {
			slog.Warn("skillkit.PluginTier: bare-name conflict, first entry wins",
				"tier", name, "skill", e.SkillName, "plugin", e.PluginName)
		}
	}

	return Tier{Name: name, Resolver: p}
}

func (p *pluginTier) Find(name string) (SkillInfo, string, bool) {
	key := p.resolveKey(name)
	if key == "" {
		return SkillInfo{}, "", false
	}
	e, ok := p.entries[key]
	if !ok {
		return SkillInfo{}, "", false
	}
	return p.load(e)
}

// resolveKey maps a bare or namespaced name to the internal key.
func (p *pluginTier) resolveKey(name string) string {
	if strings.Contains(name, ":") {
		// Namespaced: "pluginName:skillName" — already the key.
		return name
	}
	// Bare: look up in the bare index.
	return p.bare[name]
}

func (p *pluginTier) load(e PluginEntry) (SkillInfo, string, bool) {
	si := SkillInfo{
		Name:     e.SkillName,
		Path:     e.Path,
		Source:   p.name + ":" + e.PluginName,
		Metadata: Metadata{Name: e.SkillName},
	}

	if e.Body != "" {
		// Pre-loaded body: no disk read.
		meta := ParseMetadata(e.Body)
		if meta.Name != "" {
			si.Metadata = meta
		}
		return si, StripFrontmatter(e.Body), true
	}

	if e.Path != "" {
		raw, err := os.ReadFile(e.Path) //nolint:gosec // G304: plugin-supplied path
		if err != nil {
			slog.Warn("skillkit.PluginTier: cannot read skill file",
				"path", e.Path, "err", err)
			return SkillInfo{}, "", false
		}
		body := StripFrontmatter(string(raw))
		if body == "" {
			return SkillInfo{}, "", false
		}
		si.Dir = filepath.Dir(e.Path)
		meta := ParseMetadata(string(raw))
		if meta.Name != "" {
			si.Metadata = meta
		}
		return si, body, true
	}

	return SkillInfo{}, "", false
}

func (p *pluginTier) Walk(yield func(SkillInfo)) {
	for _, e := range p.entries {
		si, _, ok := p.load(e)
		if ok {
			yield(si)
		}
	}
}
