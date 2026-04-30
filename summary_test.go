package skillkit_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anatolykoptev/skillkit"
)

// ---------------------------------------------------------------------------
// BuildSummary helpers
// ---------------------------------------------------------------------------

// longDesc builds a description string of exactly n chars.
func longDesc(n int) string {
	const char = "x"
	return strings.Repeat(char, n)
}

// stubBodyWithDesc builds a minimal SKILL.md body with the given description.
func stubBodyWithDesc(name, desc string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\n---\n\n" + name + " body."
}

// ---------------------------------------------------------------------------
// BuildSummary XML
// ---------------------------------------------------------------------------

func TestBuildSummary_XML_StandardSkills(t *testing.T) {
	cat := skillkit.NewCatalog(
		skillkit.NewDirTier("builtin", filepath.Join("tier_testdata", "builtin")),
	)
	out := cat.BuildSummary(skillkit.SummaryXML)

	if !strings.Contains(out, "<skills>") {
		t.Error("XML missing <skills>")
	}
	if !strings.Contains(out, "</skills>") {
		t.Error("XML missing </skills>")
	}
	if !strings.Contains(out, `<skill name="foo"`) {
		t.Errorf("XML missing skill foo; got:\n%s", out)
	}
	if !strings.Contains(out, `<skill name="bar"`) {
		t.Errorf("XML missing skill bar; got:\n%s", out)
	}
	if !strings.Contains(out, "<description>") {
		t.Error("XML missing <description>")
	}
	if !strings.Contains(out, "</description>") {
		t.Error("XML missing </description>")
	}
}

func TestBuildSummary_XML_PluginSkillGrouped(t *testing.T) {
	entries := []skillkit.PluginEntry{
		{
			PluginName: "foo",
			SkillName:  "plugin-skill",
			Body:       "---\nname: plugin-skill\ndescription: Plugin skill desc.\n---\n\nPlugin body.",
		},
	}
	pluginTier := skillkit.NewPluginTier("plugins", entries)
	cat := skillkit.NewCatalog(pluginTier)
	out := cat.BuildSummary(skillkit.SummaryXML)

	if !strings.Contains(out, `<skill-group plugin="foo">`) {
		t.Errorf("XML missing plugin group; got:\n%s", out)
	}
	if !strings.Contains(out, "</skill-group>") {
		t.Error("XML missing </skill-group>")
	}
	if !strings.Contains(out, `<skill name="plugin-skill"`) {
		t.Errorf("XML missing plugin-skill inside group; got:\n%s", out)
	}
}

func TestBuildSummary_XML_EscapesSpecialChars(t *testing.T) {
	// Create a tier using plugin entry with special chars in description.
	entries := []skillkit.PluginEntry{
		{
			PluginName: "esc",
			SkillName:  "esc-skill",
			Body:       "---\nname: esc-skill\ndescription: a < b & c > d\n---\n\nBody.",
		},
	}
	tier := skillkit.NewPluginTier("plugins", entries)
	cat := skillkit.NewCatalog(tier)
	out := cat.BuildSummary(skillkit.SummaryXML)

	if strings.Contains(out, "a < b") {
		t.Error("XML did not escape '<' in description")
	}
	if strings.Contains(out, "c > d") {
		t.Error("XML did not escape '>' in description")
	}
	if strings.Contains(out, "b & c") {
		t.Error("XML did not escape '&' in description")
	}
	if !strings.Contains(out, "&lt;") {
		t.Error("XML missing &lt; for '<'")
	}
	if !strings.Contains(out, "&gt;") {
		t.Error("XML missing &gt; for '>'")
	}
	if !strings.Contains(out, "&amp;") {
		t.Error("XML missing &amp; for '&'")
	}
}

func TestBuildSummary_XML_DescriptionTruncatedAt180(t *testing.T) {
	desc := longDesc(200) //nolint:mnd
	entries := []skillkit.PluginEntry{
		{
			PluginName: "p",
			SkillName:  "long-skill",
			Body:       stubBodyWithDesc("long-skill", desc),
		},
	}
	tier := skillkit.NewPluginTier("plugins", entries)
	cat := skillkit.NewCatalog(tier)
	out := cat.BuildSummary(skillkit.SummaryXML)

	// Truncated description should be 180 x's, not 200.
	truncated := strings.Repeat("x", 180) //nolint:mnd
	if !strings.Contains(out, truncated) {
		t.Error("XML description not truncated to 180 chars")
	}
	full := strings.Repeat("x", 200) //nolint:mnd
	if strings.Contains(out, full) {
		t.Error("XML description not truncated; still contains 200-char string")
	}
}

// ---------------------------------------------------------------------------
// BuildSummary Markdown
// ---------------------------------------------------------------------------

func TestBuildSummary_Markdown_Format(t *testing.T) {
	cat := skillkit.NewCatalog(
		skillkit.NewDirTier("builtin", filepath.Join("tier_testdata", "builtin")),
	)
	out := cat.BuildSummary(skillkit.SummaryMarkdown)

	if !strings.Contains(out, "### foo") {
		t.Errorf("Markdown missing '### foo'; got:\n%s", out)
	}
	if !strings.Contains(out, "### bar") {
		t.Errorf("Markdown missing '### bar'; got:\n%s", out)
	}
	if !strings.Contains(out, "Builtin foo skill") {
		t.Errorf("Markdown missing foo description; got:\n%s", out)
	}
}

func TestBuildSummary_Markdown_DescriptionTruncatedAt180(t *testing.T) {
	desc := longDesc(200) //nolint:mnd
	entries := []skillkit.PluginEntry{
		{
			PluginName: "p",
			SkillName:  "long-md",
			Body:       stubBodyWithDesc("long-md", desc),
		},
	}
	tier := skillkit.NewPluginTier("plugins", entries)
	cat := skillkit.NewCatalog(tier)
	out := cat.BuildSummary(skillkit.SummaryMarkdown)

	truncated := strings.Repeat("x", 180) //nolint:mnd
	if !strings.Contains(out, truncated) {
		t.Errorf("Markdown description not truncated to 180; got:\n%s", out)
	}
	full := strings.Repeat("x", 200) //nolint:mnd
	if strings.Contains(out, full) {
		t.Error("Markdown description not truncated; still contains 200-char string")
	}
}

// ---------------------------------------------------------------------------
// BuildSummary JSON
// ---------------------------------------------------------------------------

func TestBuildSummary_JSON_ValidRoundtrip(t *testing.T) {
	cat := skillkit.NewCatalog(
		skillkit.NewDirTier("builtin", filepath.Join("tier_testdata", "builtin")),
	)
	out := cat.BuildSummary(skillkit.SummaryJSON)

	var skills []skillkit.SkillInfo
	if err := json.Unmarshal([]byte(out), &skills); err != nil {
		t.Fatalf("JSON unmarshal failed: %v\noutput: %s", err, out)
	}
	if len(skills) == 0 {
		t.Error("JSON roundtrip yielded empty slice")
	}
}

func TestBuildSummary_JSON_FullDescription(t *testing.T) {
	desc := longDesc(200) //nolint:mnd
	entries := []skillkit.PluginEntry{
		{
			PluginName: "p",
			SkillName:  "full-json",
			Body:       stubBodyWithDesc("full-json", desc),
		},
	}
	tier := skillkit.NewPluginTier("plugins", entries)
	cat := skillkit.NewCatalog(tier)
	out := cat.BuildSummary(skillkit.SummaryJSON)

	full := strings.Repeat("x", 200) //nolint:mnd
	if !strings.Contains(out, full) {
		t.Errorf("JSON description truncated; want full 200 chars. Got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// BuildSummary empty Catalog
// ---------------------------------------------------------------------------

func TestBuildSummary_EmptyCatalog_ReturnsEmpty(t *testing.T) {
	cat := skillkit.NewCatalog()

	for _, format := range []skillkit.SummaryFormat{
		skillkit.SummaryXML, skillkit.SummaryMarkdown, skillkit.SummaryJSON,
	} {
		out := cat.BuildSummary(format)
		if out != "" {
			t.Errorf("BuildSummary(%v) with no tiers = %q, want ''", format, out)
		}
	}
}
