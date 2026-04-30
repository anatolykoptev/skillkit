package skill

import (
	"bufio"
	"strings"
	"unicode/utf8"
)

const scannerBufSize = 1 << 20 // 1 MiB — prevents silent truncation of large skills

// StripFrontmatter removes a leading YAML or JSON frontmatter block
// and returns the body trimmed of leading whitespace (spaces, tabs,
// and newlines). Input without a recognized fence is returned
// unchanged. Supports YAML ("---" fences) and JSON (top-level object
// on first line, blank line, body).
//
// CRLF input is normalized to LF before parsing. The "unchanged"
// guarantee covers byte-identical input only for content that does NOT
// start with '-' or '{'; once a fence is detected and parsing fails
// (e.g. "---\r\n" with no closing fence), the returned string is the
// CRLF-normalized form. NUL bytes inside the body are silently
// stripped (bufio.Scanner stops at NUL otherwise).
func StripFrontmatter(content string) string {
	_, body := ParseFrontmatter(content)
	return body
}

// ParseFrontmatter splits content into (frontmatter, body). Returns
// ("", content) when no fence is found.
func ParseFrontmatter(content string) (frontmatter, body string) {
	// Fast path: no allocations if no fence detected.
	if len(content) == 0 {
		return "", content
	}
	first := content[0]
	if first != '-' && first != '{' {
		return "", content
	}

	// Normalize CRLF once before scanning.
	normalized := strings.ReplaceAll(content, "\r\n", "\n")

	if normalized[0] == '{' {
		return parseJSONFrontmatter(normalized)
	}
	return parseYAMLFrontmatter(normalized)
}

// parseYAMLFrontmatter handles "---\n...\n---\nbody" format.
func parseYAMLFrontmatter(content string) (frontmatter, body string) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

	// First line must be exactly "---".
	if !scanner.Scan() || scanner.Text() != "---" {
		return "", content
	}

	var fm strings.Builder
	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			return fm.String(), collectBody(scanner)
		}
		if fm.Len() > 0 {
			fm.WriteByte('\n')
		}
		fm.WriteString(line)
	}

	// No closing fence found — return content unchanged.
	return "", content
}

// parseJSONFrontmatter handles "{...}\n\nbody" format.
func parseJSONFrontmatter(content string) (frontmatter, body string) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	scanner.Buffer(make([]byte, scannerBufSize), scannerBufSize)

	jsonLines, ok := scanJSONBlock(scanner)
	if !ok {
		return "", content
	}
	fm := strings.Join(jsonLines, "\n")

	// Next line must be blank separator.
	if !scanner.Scan() {
		return fm, ""
	}
	if scanner.Text() != "" {
		return "", content
	}

	return fm, collectBody(scanner)
}

// scanJSONBlock reads lines until brace depth returns to zero.
// Returns (lines, true) on success, (nil, false) when depth never balances.
func scanJSONBlock(scanner *bufio.Scanner) ([]string, bool) {
	var lines []string
	depth := 0
	for scanner.Scan() {
		line := scanner.Text()
		lines = append(lines, line)
		depth += braceDepthDelta(line)
		if depth == 0 {
			return lines, true
		}
	}
	return nil, false
}

// braceDepthDelta counts net '{' minus '}' in a line.
func braceDepthDelta(line string) int {
	delta := 0
	for _, ch := range line {
		switch ch {
		case '{':
			delta++
		case '}':
			delta--
		}
	}
	return delta
}

// collectBody drains scanner lines into a NUL-free body string with
// leading whitespace (spaces, tabs, newlines) stripped per the spec.
func collectBody(scanner *bufio.Scanner) string {
	var bodyLines []string
	for scanner.Scan() {
		bodyLines = append(bodyLines, scanner.Text())
	}
	return stripNUL(strings.TrimLeft(strings.Join(bodyLines, "\n"), " \t\n"))
}

// stripNUL removes NUL bytes from s. bufio.Scanner stops at NUL,
// so skill bodies with embedded NULs must be sanitized.
func stripNUL(s string) string {
	if !strings.ContainsRune(s, 0) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r != 0 {
			b.WriteRune(r)
		}
		i += size
	}
	return b.String()
}
