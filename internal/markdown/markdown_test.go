package markdown

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseBlanksFrontmatterCommentsAndCode(t *testing.T) {
	src := "---\nid: x\n---\n# Title\n<!-- hidden\n## Not a heading -->\nkept <!-- inline --> text\n```go\n## code\n```\n## Real\r\n"
	d, err := Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	if !d.HasFrontmatter || d.Frontmatter != "id: x" || d.FrontmatterLine != 2 {
		t.Errorf("frontmatter = %q (has=%v line=%d)", d.Frontmatter, d.HasFrontmatter, d.FrontmatterLine)
	}
	if got := d.Lines[6].Text; got != "kept  text" {
		t.Errorf("inline comment not stripped: %q", got)
	}
	if !reflect.DeepEqual(d.CodeLines, []int{8, 9, 10}) {
		t.Errorf("code lines = %v", d.CodeLines)
	}
	secs := d.Sections(2)
	if len(secs) != 1 || secs[0].Title != "Real" || secs[0].Line != 11 {
		t.Errorf("sections = %+v", secs)
	}
}

func TestParseUnterminatedFrontmatter(t *testing.T) {
	if _, err := Parse([]byte("---\nid: x\n# no end\n")); !errors.Is(err, ErrUnterminatedFrontmatter) {
		t.Fatalf("err = %v", err)
	}
}

func TestSectionsIn(t *testing.T) {
	d, _ := Parse([]byte("intro\n## A\na1\n### A.1\nsub\n## B\nb1\n# Top\nlost\n## C\n"))
	pre, secs := SectionsIn(d.Lines, 2)
	if len(pre) != 1 || pre[0].Text != "intro" {
		t.Errorf("preamble = %+v", pre)
	}
	var titles []string
	for _, s := range secs {
		titles = append(titles, s.Title)
	}
	if !reflect.DeepEqual(titles, []string{"A", "B", "C"}) {
		t.Fatalf("titles = %v", titles)
	}
	if len(secs[0].Body) != 3 {
		t.Errorf("A should include its subsection, got %+v", secs[0].Body)
	}
	if len(secs[1].Body) != 1 {
		t.Errorf("a higher-ranked heading must end B, got %+v", secs[1].Body)
	}
}

func TestTable(t *testing.T) {
	d, _ := Parse([]byte("text\n| ID | Comp |\n|---|:-:|\n| FR-001 | A, `B` |\n| FR-002 | x \\| y |\n\nafter | not a row\n"))
	header, rows := Table(d.Lines)
	if !reflect.DeepEqual(header, []string{"ID", "Comp"}) {
		t.Errorf("header = %v", header)
	}
	if len(rows) != 2 || rows[0].N != 4 || rows[1].Cells[1] != "x | y" {
		t.Fatalf("rows = %+v", rows)
	}
	if got := SplitList(rows[0].Cells[1]); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Errorf("SplitList = %v", got)
	}
}

func TestBulletAndInlineList(t *testing.T) {
	tests := []struct {
		in      string
		indent  int
		content string
		ok      bool
	}{
		{"- a", 0, "a", true},
		{"  * b ", 2, "b", true},
		{"+ c", 0, "c", true},
		{"-no space", 0, "", false},
		{"text", 0, "", false},
	}
	for _, tt := range tests {
		indent, content, ok := Bullet(tt.in)
		if indent != tt.indent || content != tt.content || ok != tt.ok {
			t.Errorf("Bullet(%q) = %d %q %v", tt.in, indent, content, ok)
		}
	}
	if got := InlineList("[G1, G2]"); !reflect.DeepEqual(got, []string{"G1", "G2"}) {
		t.Errorf("InlineList = %v", got)
	}
	if got := InlineList("[]"); len(got) != 0 {
		t.Errorf("empty list = %v", got)
	}
}

func TestSetFrontmatter(t *testing.T) {
	tests := []struct {
		name, in, want string
	}{
		{"replace", "---\nid: x\nhash: \"\"  \n---\nbody\n", "---\nid: x\nhash: \"sha256:1\"\n---\nbody\n"},
		{"insert", "---\nid: x\n---\nbody\n", "---\nid: x\nhash: \"sha256:1\"\n---\nbody\n"},
		{"crlf", "---\r\nhash: old\r\n---\r\n", "---\r\nhash: \"sha256:1\"\r\n---\r\n"},
		{"nested key untouched", "---\nc:\n  hash: keep\n---\n", "---\nc:\n  hash: keep\nhash: \"sha256:1\"\n---\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := SetFrontmatter([]byte(tt.in), "hash", "sha256:1")
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tt.want {
				t.Errorf("got:\n%q\nwant:\n%q", got, tt.want)
			}
		})
	}
	if _, err := SetFrontmatter([]byte("no frontmatter"), "k", "v"); err == nil || !strings.Contains(err.Error(), "---") {
		t.Errorf("want an error without frontmatter, got %v", err)
	}
}
