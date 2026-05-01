package skillkit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/skillkit"
)

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// makeLocaleDir creates a tier root under t.TempDir() with multiple locale
// variants of the same skill. Each variant is a subdir with a SKILL.md file.
//
//	subdirName: the on-disk directory name (e.g. "greeter-ru")
//	frontmatterName: the `name:` field in frontmatter (e.g. "greeter")
//	locale: the `locale:` field (may be "")
//	body: text after frontmatter
func addLocaleVariant(t *testing.T, root, subdirName, frontmatterName, locale, body string) {
	t.Helper()
	dir := filepath.Join(root, subdirName)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("MkdirAll(%s): %v", dir, err)
	}
	localeField := ""
	if locale != "" {
		localeField = "\nlocale: " + locale
	}
	content := "---\nname: " + frontmatterName + "\ndescription: " + frontmatterName + " " + locale + " variant." + localeField + "\n---\n\n" + body
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile SKILL.md: %v", err)
	}
}

// ---------------------------------------------------------------------------
// TestCatalog_WithLocale_PrefersExactMatch
// ---------------------------------------------------------------------------

func TestCatalog_WithLocale_PrefersExactMatch(t *testing.T) {
	root := t.TempDir()
	// Three skills with same frontmatter name "greeter", different locales.
	addLocaleVariant(t, root, "greeter-en", "greeter", "en", "Hello world.")
	addLocaleVariant(t, root, "greeter-ru", "greeter", "ru", "Привет мир.")
	addLocaleVariant(t, root, "greeter-neutral", "greeter", "", "Generic greeting.")

	cat := skillkit.NewCatalog(skillkit.NewDirTier("t", root)).WithLocale("ru")
	body, info, ok := cat.Load("greeter")
	if !ok {
		t.Fatal("Load returned ok=false, want true")
	}
	if !strings.Contains(body, "Привет мир.") {
		t.Errorf("body = %q, want Russian variant", body)
	}
	if info.Locale != "ru" {
		t.Errorf("Metadata.Locale = %q, want 'ru'", info.Locale)
	}
}

// ---------------------------------------------------------------------------
// TestCatalog_WithLocale_FallsBackToNeutral
// ---------------------------------------------------------------------------

func TestCatalog_WithLocale_FallsBackToNeutral(t *testing.T) {
	root := t.TempDir()
	// en variant + neutral variant, no ru variant.
	addLocaleVariant(t, root, "greeter-en", "greeter", "en", "Hello world.")
	addLocaleVariant(t, root, "greeter-neutral", "greeter", "", "Generic greeting.")

	cat := skillkit.NewCatalog(skillkit.NewDirTier("t", root)).WithLocale("ru")
	body, info, ok := cat.Load("greeter")
	if !ok {
		t.Fatal("Load returned ok=false, want true")
	}
	if !strings.Contains(body, "Generic greeting.") {
		t.Errorf("body = %q, want neutral variant", body)
	}
	if info.Locale != "" {
		t.Errorf("Metadata.Locale = %q, want '' (neutral)", info.Locale)
	}
}

// ---------------------------------------------------------------------------
// TestCatalog_WithLocale_FallsBackToAnyMatch
// ---------------------------------------------------------------------------

func TestCatalog_WithLocale_FallsBackToAnyMatch(t *testing.T) {
	root := t.TempDir()
	// Only en and zh variants — no ru, no neutral.
	addLocaleVariant(t, root, "greeter-en", "greeter", "en", "Hello world.")
	addLocaleVariant(t, root, "greeter-zh", "greeter", "zh", "你好世界。")

	cat := skillkit.NewCatalog(skillkit.NewDirTier("t", root)).WithLocale("ru")
	_, _, ok := cat.Load("greeter")
	// Must find something (any match), not miss.
	if !ok {
		t.Fatal("Load returned ok=false; expected any-match fallback to return ok=true")
	}
}

// ---------------------------------------------------------------------------
// TestCatalog_WithLocale_EmptyLocale_NoFiltering
// ---------------------------------------------------------------------------

func TestCatalog_WithLocale_EmptyLocale_NoFiltering(t *testing.T) {
	// WithLocale("") should be a no-op — same behavior as plain catalog.
	root := t.TempDir()
	addLocaleVariant(t, root, "greeter-en", "greeter", "en", "Hello world.")

	cat := skillkit.NewCatalog(skillkit.NewDirTier("t", root)).WithLocale("")
	if cat.Locale() != "" {
		t.Errorf("Locale() = %q after WithLocale(''), want ''", cat.Locale())
	}
	// Load still works via plain Find path.
	_, _, ok := cat.Load("greeter-en")
	if !ok {
		t.Fatal("Load(greeter-en) returned ok=false")
	}
}

// ---------------------------------------------------------------------------
// TestCatalog_WithLocale_CaseInsensitive
// ---------------------------------------------------------------------------

func TestCatalog_WithLocale_CaseInsensitive(t *testing.T) {
	root := t.TempDir()
	// Skill has locale: ru (lowercase), catalog configured with "RU" (uppercase).
	addLocaleVariant(t, root, "greeter-ru", "greeter", "ru", "Привет мир.")
	addLocaleVariant(t, root, "greeter-neutral", "greeter", "", "Generic greeting.")

	cat := skillkit.NewCatalog(skillkit.NewDirTier("t", root)).WithLocale("RU")
	body, _, ok := cat.Load("greeter")
	if !ok {
		t.Fatal("Load returned ok=false")
	}
	if !strings.Contains(body, "Привет мир.") {
		t.Errorf("body = %q, want Russian variant (case-insensitive match)", body)
	}
}

// ---------------------------------------------------------------------------
// TestCatalog_WithLocale_NoMatch_ReturnsFalse
// ---------------------------------------------------------------------------

func TestCatalog_WithLocale_NoMatch_ReturnsFalse(t *testing.T) {
	root := t.TempDir()
	addLocaleVariant(t, root, "greeter-en", "greeter", "en", "Hello world.")

	cat := skillkit.NewCatalog(skillkit.NewDirTier("t", root)).WithLocale("ru")
	_, _, ok := cat.Load("nonexistent-skill")
	if ok {
		t.Error("Load(nonexistent) returned ok=true; want false")
	}
}

// ---------------------------------------------------------------------------
// TestCatalog_WithLocale_ChainsWithObserver
// ---------------------------------------------------------------------------

func TestCatalog_WithLocale_ChainsWithObserver(t *testing.T) {
	root := t.TempDir()
	addLocaleVariant(t, root, "greeter-ru", "greeter", "ru", "Привет мир.")

	hitCount := 0
	obs := &skillkit.Observer{
		CatalogLoad: func(_, outcome string) {
			if outcome == "hit" {
				hitCount++
			}
		},
	}

	// Both chained methods must return *Catalog and work together.
	cat := skillkit.NewCatalog(skillkit.NewDirTier("t", root)).
		WithObserver(obs).
		WithLocale("ru")

	body, _, ok := cat.Load("greeter")
	if !ok {
		t.Fatal("Load returned ok=false")
	}
	if !strings.Contains(body, "Привет мир.") {
		t.Errorf("body = %q, want Russian variant", body)
	}
	if hitCount == 0 {
		t.Error("Observer.CatalogLoad was not called")
	}
}

// ---------------------------------------------------------------------------
// TestCatalog_Locale_Getter
// ---------------------------------------------------------------------------

func TestCatalog_Locale_Getter(t *testing.T) {
	cat := skillkit.NewCatalog().WithLocale("zh")
	if got := cat.Locale(); got != "zh" {
		t.Errorf("Locale() = %q, want 'zh'", got)
	}
}

func TestCatalog_Locale_GetterDefault(t *testing.T) {
	cat := skillkit.NewCatalog()
	if got := cat.Locale(); got != "" {
		t.Errorf("Locale() = %q, want '' (default)", got)
	}
}

// ---------------------------------------------------------------------------
// TestCatalog_NoLocale_BackwardCompat
// ---------------------------------------------------------------------------

func TestCatalog_NoLocale_BackwardCompat(t *testing.T) {
	// Catalog without WithLocale must behave exactly as in v0.2.0.
	cat := skillkit.NewCatalog(builtinTier())

	body, info, ok := cat.Load("foo")
	if !ok {
		t.Fatal("Load(foo) returned ok=false")
	}
	if !strings.Contains(body, "Builtin foo body.") {
		t.Errorf("body = %q", body)
	}
	if info.Name != "foo" {
		t.Errorf("info.Name = %q", info.Name)
	}

	_, _, ok2 := cat.Load("nonexistent")
	if ok2 {
		t.Error("Load(nonexistent) returned ok=true")
	}
}

// ---------------------------------------------------------------------------
// TestCatalog_WithLocale_EmbedFSTier
// ---------------------------------------------------------------------------

// TestCatalog_WithLocale_EmbedFSTier verifies locale routing works with
// EmbedFSTier using a DirTier fallback (embed.FS doesn't support runtime
// variant injection, but the code path through ResolveByInfo is exercised
// via DirTier in the other tests).
func TestCatalog_WithLocale_EmbedFSTier_ReturnsNeutral(t *testing.T) {
	// EmbedFSTier fixture has "baz" with no locale field → neutral.
	// WithLocale("ru") should fall back to neutral.
	cat := skillkit.NewCatalog(
		skillkit.NewEmbedFSTier("embed", embedTestFS, "tier_testdata/embedfs"),
	).WithLocale("ru")

	body, info, ok := cat.Load("baz")
	if !ok {
		t.Fatal("Load(baz) returned ok=false")
	}
	if !strings.Contains(body, "Embedded baz body.") {
		t.Errorf("body = %q", body)
	}
	if info.Locale != "" {
		t.Errorf("Metadata.Locale = %q, want '' (neutral)", info.Locale)
	}
}

// ---------------------------------------------------------------------------
// TestCatalog_WithLocale_PluginTier
// ---------------------------------------------------------------------------

func TestCatalog_WithLocale_PluginTier_PrefersExactLocale(t *testing.T) {
	entries := []skillkit.PluginEntry{
		{
			PluginName: "myplugin",
			SkillName:  "greeter-en",
			Body:       "---\nname: greeter\ndescription: English greeter.\nlocale: en\n---\n\nHello world.",
		},
		{
			PluginName: "myplugin",
			SkillName:  "greeter-ru",
			Body:       "---\nname: greeter\ndescription: Russian greeter.\nlocale: ru\n---\n\nПривет мир.",
		},
	}
	tier := skillkit.NewPluginTier("plugins", entries)
	cat := skillkit.NewCatalog(tier).WithLocale("ru")

	body, info, ok := cat.Load("greeter")
	if !ok {
		t.Fatal("Load(greeter) returned ok=false")
	}
	if !strings.Contains(body, "Привет мир.") {
		t.Errorf("body = %q, want Russian variant", body)
	}
	if info.Locale != "ru" {
		t.Errorf("Metadata.Locale = %q, want 'ru'", info.Locale)
	}
}

// ---------------------------------------------------------------------------
// TestCatalog_WithLocale_MultiTier_LocaleInSecondTier
// ---------------------------------------------------------------------------

func TestCatalog_WithLocale_MultiTier_LocaleInSecondTier(t *testing.T) {
	// First tier has neutral "greeter", second tier has ru "greeter".
	// WithLocale("ru") should still use first tier (which has a neutral match)
	// over second tier — tier priority wins, locale preference is within-tier.
	root1 := t.TempDir()
	root2 := t.TempDir()
	addLocaleVariant(t, root1, "greeter-neutral", "greeter", "", "Generic greeting.")
	addLocaleVariant(t, root2, "greeter-ru", "greeter", "ru", "Привет мир.")

	cat := skillkit.NewCatalog(
		skillkit.NewDirTier("tier1", root1),
		skillkit.NewDirTier("tier2", root2),
	).WithLocale("ru")

	body, info, ok := cat.Load("greeter")
	if !ok {
		t.Fatal("Load returned ok=false")
	}
	// Tier1 has a neutral match → should win over tier2's ru match.
	if !strings.Contains(body, "Generic greeting.") {
		t.Errorf("body = %q, want tier1 neutral (tier priority wins)", body)
	}
	if info.Source != "tier1" {
		t.Errorf("Source = %q, want 'tier1'", info.Source)
	}
}

// ---------------------------------------------------------------------------
// TestCatalog_WithLocale_CustomResolver_BestEffortFallback
//
// A Resolver that does NOT implement bodyByInfoResolver. The catalog must
// still return a result via the best-effort Find fallback (slog.Debug path).
// ---------------------------------------------------------------------------

// localeStubResolver is a minimal Resolver that knows about locale variants
// but does NOT implement bodyByInfoResolver. Used to exercise the fallback
// code path in findInTierByLocale.
type localeStubResolver struct {
	skills []skillkit.SkillInfo
	bodies map[string]string // keyed by SkillInfo.Name (subdir equiv)
}

func (r localeStubResolver) Find(name string) (skillkit.SkillInfo, string, bool) {
	for _, si := range r.skills {
		if si.Name == name || si.Metadata.Name == name {
			return si, r.bodies[si.Name], true
		}
	}
	return skillkit.SkillInfo{}, "", false
}

func (r localeStubResolver) Walk(yield func(skillkit.SkillInfo)) {
	for _, si := range r.skills {
		yield(si)
	}
}

func TestCatalog_WithLocale_CustomResolver_BestEffortFallback(t *testing.T) {
	ruInfo := skillkit.SkillInfo{Name: "greeter", Metadata: skillkit.Metadata{Name: "greeter", Locale: "ru", Description: "ru greeter"}}
	stub := localeStubResolver{
		skills: []skillkit.SkillInfo{ruInfo},
		bodies: map[string]string{"greeter": "Привет мир."},
	}
	tier := skillkit.Tier{Name: "stub", Resolver: stub}
	cat := skillkit.NewCatalog(tier).WithLocale("ru")

	// The stub doesn't implement bodyByInfoResolver; locale routing falls
	// back to plain Find, which returns the first name match.
	body, _, ok := cat.Load("greeter")
	if !ok {
		t.Fatal("Load returned ok=false; expected best-effort fallback to succeed")
	}
	if body == "" {
		t.Error("body is empty; expected best-effort fallback to return body")
	}
}

// ---------------------------------------------------------------------------
// TestCatalog_WithLocale_ResolveByInfo_EmptyPath_FallsBackToFind
//
// DirTier.ResolveByInfo returns false when info.Path is empty. The catalog
// must fall back to plain Find rather than returning ok=false.
// ---------------------------------------------------------------------------

func TestCatalog_WithLocale_ResolveByInfo_EmptyPath_FallsBackToFind(t *testing.T) {
	root := t.TempDir()
	// Normal skill with no locale — DirTier will set a valid Path.
	// We use a PluginTier entry with Body-only (no Path) to exercise
	// the empty-Path branch of ResolveByInfo.
	entries := []skillkit.PluginEntry{
		{
			PluginName: "myplugin",
			SkillName:  "body-only",
			Body:       "---\nname: body-only\ndescription: Body only skill.\nlocale: en\n---\n\nBody only content.",
		},
	}
	_ = root
	tier := skillkit.NewPluginTier("plugins", entries)
	cat := skillkit.NewCatalog(tier).WithLocale("en")

	body, _, ok := cat.Load("body-only")
	if !ok {
		t.Fatal("Load returned ok=false")
	}
	if !strings.Contains(body, "Body only content.") {
		t.Errorf("body = %q, want body-only content", body)
	}
}
