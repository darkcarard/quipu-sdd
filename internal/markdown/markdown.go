// Package markdown is the small, line-oriented markdown reader shared by the
// spec and plan parsers. It understands YAML frontmatter, ATX headings,
// bullets, pipe tables, HTML comments and fenced code blocks; nothing else.
//
// Line numbers are preserved everywhere so diagnostics can point at file:line.
package markdown

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Line is one line of a document with its 1-based line number.
type Line struct {
	N    int
	Text string
}

// Doc is a parsed document. Lines covers the whole file: frontmatter lines,
// fenced code and HTML comments are blanked so that structural parsing never
// sees them, while line numbers stay aligned with the file.
type Doc struct {
	// Frontmatter is the raw YAML between the leading "---" lines ("" if none).
	Frontmatter    string
	HasFrontmatter bool
	// FrontmatterLine is the file line of the first YAML line.
	FrontmatterLine int
	Lines           []Line
	// CodeLines are the line numbers that belong to fenced code blocks.
	CodeLines []int
}

// ErrUnterminatedFrontmatter is returned when a leading "---" has no closing "---".
var ErrUnterminatedFrontmatter = errors.New("frontmatter: missing closing ---")

// Parse reads content into a Doc.
func Parse(content []byte) (*Doc, error) {
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	raw := strings.Split(text, "\n")
	if len(raw) > 0 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	d := &Doc{Lines: make([]Line, len(raw))}
	for i := range raw {
		d.Lines[i].N = i + 1
	}

	start := 0
	if len(raw) > 0 && strings.TrimSpace(raw[0]) == "---" {
		end := -1
		for i := 1; i < len(raw); i++ {
			if t := strings.TrimSpace(raw[i]); t == "---" || t == "..." {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, ErrUnterminatedFrontmatter
		}
		d.HasFrontmatter = true
		d.FrontmatterLine = 2
		d.Frontmatter = strings.Join(raw[1:end], "\n")
		start = end + 1
	}

	inComment, inFence := false, false
	fence := ""
	for i := start; i < len(raw); i++ {
		line := raw[i]
		trimmed := strings.TrimSpace(line)
		if !inComment {
			if inFence {
				d.CodeLines = append(d.CodeLines, i+1)
				if strings.HasPrefix(trimmed, fence) && strings.Trim(trimmed, fence[:1]) == "" {
					inFence = false
				}
				continue
			}
			if f := fenceMarker(trimmed); f != "" {
				inFence, fence = true, f
				d.CodeLines = append(d.CodeLines, i+1)
				continue
			}
		}
		d.Lines[i].Text, inComment = stripComments(line, inComment)
	}
	return d, nil
}

func fenceMarker(trimmed string) string {
	for _, f := range []string{"```", "~~~"} {
		if strings.HasPrefix(trimmed, f) {
			n := len(trimmed) - len(strings.TrimLeft(trimmed, f[:1]))
			return strings.Repeat(f[:1], n)
		}
	}
	return ""
}

// stripComments removes <!-- --> segments from line, carrying the open-comment state across lines.
func stripComments(line string, inComment bool) (string, bool) {
	var sb strings.Builder
	for line != "" {
		if inComment {
			end := strings.Index(line, "-->")
			if end < 0 {
				return sb.String(), true
			}
			line, inComment = line[end+3:], false
			continue
		}
		open := strings.Index(line, "<!--")
		if open < 0 {
			sb.WriteString(line)
			break
		}
		sb.WriteString(line[:open])
		line, inComment = line[open+4:], true
	}
	return sb.String(), inComment
}

var headingRe = regexp.MustCompile(`^(#{1,6})\s+(.*?)\s*#*\s*$`)

// Heading reports whether text is an ATX heading and returns its level and title.
func Heading(text string) (level int, title string, ok bool) {
	m := headingRe.FindStringSubmatch(text)
	if m == nil {
		return 0, "", false
	}
	return len(m[1]), m[2], true
}

// Section is a heading and the lines below it, up to the next heading of the
// same or a higher rank. Body includes any deeper subheadings.
type Section struct {
	Level int
	Title string
	Line  int
	Body  []Line
}

// Sections returns the sections of exactly level in the whole document.
func (d *Doc) Sections(level int) []Section {
	_, secs := SectionsIn(d.Lines, level)
	return secs
}

// SectionsIn splits lines into sections of exactly level. Lines before the
// first such heading are returned as preamble. A heading of a higher rank
// (fewer #) ends the current section and is not returned.
func SectionsIn(lines []Line, level int) (preamble []Line, secs []Section) {
	cur := -1
	for _, l := range lines {
		if lv, title, ok := Heading(l.Text); ok && lv <= level {
			if lv == level {
				secs = append(secs, Section{Level: lv, Title: title, Line: l.N})
				cur = len(secs) - 1
			} else {
				cur = -2 // inside a higher-ranked heading: drop lines until the next section
			}
			continue
		}
		switch cur {
		case -1:
			preamble = append(preamble, l)
		case -2:
		default:
			secs[cur].Body = append(secs[cur].Body, l)
		}
	}
	return preamble, secs
}

// Blank reports whether every line is empty after trimming.
func Blank(lines []Line) bool {
	for _, l := range lines {
		if strings.TrimSpace(l.Text) != "" {
			return false
		}
	}
	return true
}

var bulletRe = regexp.MustCompile(`^(\s*)[-*+]\s+(.*)$`)

// Bullet reports whether text is a list item and returns its indentation and content.
func Bullet(text string) (indent int, content string, ok bool) {
	m := bulletRe.FindStringSubmatch(text)
	if m == nil {
		return 0, "", false
	}
	return len(m[1]), strings.TrimSpace(m[2]), true
}

// Row is one data row of a pipe table.
type Row struct {
	N     int
	Cells []string
}

var separatorCell = regexp.MustCompile(`^:?-{1,}:?$`)

// Table returns the header and data rows of the first pipe table in lines.
func Table(lines []Line) (header []string, rows []Row) {
	var tbl []Row
	started := false
	for _, l := range lines {
		t := strings.TrimSpace(l.Text)
		if strings.HasPrefix(t, "|") {
			started = true
			tbl = append(tbl, Row{N: l.N, Cells: splitRow(t)})
			continue
		}
		if started {
			break
		}
	}
	if len(tbl) >= 2 && isSeparator(tbl[1].Cells) {
		return tbl[0].Cells, tbl[2:]
	}
	return nil, tbl
}

func isSeparator(cells []string) bool {
	for _, c := range cells {
		if !separatorCell.MatchString(strings.ReplaceAll(c, " ", "")) {
			return false
		}
	}
	return len(cells) > 0
}

func splitRow(t string) []string {
	t = strings.TrimPrefix(t, "|")
	if strings.HasSuffix(t, "|") && !strings.HasSuffix(t, `\|`) {
		t = t[:len(t)-1]
	}
	var cells []string
	var sb strings.Builder
	for i := 0; i < len(t); i++ {
		switch {
		case t[i] == '\\' && i+1 < len(t) && t[i+1] == '|':
			sb.WriteByte('|')
			i++
		case t[i] == '|':
			cells = append(cells, strings.TrimSpace(sb.String()))
			sb.Reset()
		default:
			sb.WriteByte(t[i])
		}
	}
	return append(cells, strings.TrimSpace(sb.String()))
}

// SplitList splits a comma/semicolon separated list (also accepting <br>), trims
// each item and strips surrounding backticks. Empty items are dropped.
func SplitList(s string) []string {
	s = strings.NewReplacer("<br>", ",", "<br/>", ",", "<br />", ",", ";", ",").Replace(s)
	var out []string
	for _, p := range strings.Split(s, ",") {
		p = strings.Trim(strings.TrimSpace(p), "`")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// InlineList parses a bracketed list such as "[a, b]" (brackets optional).
func InlineList(s string) []string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	return SplitList(s)
}

// SetFrontmatter sets a top-level scalar key in the YAML frontmatter of content,
// editing only that line (or inserting it before the closing ---) so comments
// and formatting are preserved. The value is written double-quoted.
func SetFrontmatter(content []byte, key, value string) ([]byte, error) {
	lines := strings.SplitAfter(string(content), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return nil, errors.New("frontmatter: document does not start with ---")
	}
	keyRe := regexp.MustCompile(`^` + regexp.QuoteMeta(key) + `\s*:`)
	for i := 1; i < len(lines); i++ {
		t := strings.TrimRight(lines[i], "\r\n")
		eol := lines[i][len(t):]
		if eol == "" {
			eol = "\n"
		}
		if trimmed := strings.TrimSpace(t); trimmed == "---" || trimmed == "..." {
			lines[i] = fmt.Sprintf("%s: %q%s", key, value, eol) + lines[i]
			return []byte(strings.Join(lines, "")), nil
		}
		if keyRe.MatchString(t) {
			lines[i] = fmt.Sprintf("%s: %q%s", key, value, eol)
			return []byte(strings.Join(lines, "")), nil
		}
	}
	return nil, ErrUnterminatedFrontmatter
}
