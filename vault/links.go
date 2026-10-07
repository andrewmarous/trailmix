package vault

import (
	"strings"
)

// Link is an Obsidian wiki link. Target excludes the fragment and display alias.
// Line is the one-based source line. Embed identifies ![[...]] references.
// Fragment preserves heading or block references without the leading #.
type Link struct {
	Target   string `json:"target"`
	Fragment string `json:"fragment,omitempty"`
	Alias    string `json:"alias,omitempty"`
	Line     int    `json:"line"`
	Embed    bool   `json:"embed,omitempty"`
}

// ParseLinks extracts wiki links outside fenced code, inline code, and HTML
// comments. It preserves source order and duplicates and does not resolve paths.
// Links cannot span lines. Escaped opening brackets are ignored.
func ParseLinks(content string) []Link {
	links := []Link{}
	var fence byte
	fenceLength := 0
	comment := false
	inline := 0
	offset := 0
	for lineNumber, line := range strings.Split(content, "\n") {
		lineStart := offset
		offset += len(line) + 1
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		if !comment && inline == 0 && indent <= 3 && len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			n := runLength(trimmed, 0, trimmed[0])
			if fence != 0 {
				if trimmed[0] == fence && n >= fenceLength && strings.TrimSpace(trimmed[n:]) == "" {
					fence = 0
					fenceLength = 0
				}
				continue
			}
			if n >= 3 && (trimmed[0] != '`' || !strings.Contains(trimmed[n:], "`")) {
				fence = trimmed[0]
				fenceLength = n
				continue
			}
		}
		if fence != 0 {
			continue
		}
		for i := 0; i < len(line); {
			if comment {
				end := strings.Index(line[i:], "-->")
				if end < 0 {
					break
				}
				i += end + 3
				comment = false
				continue
			}
			if inline != 0 {
				if line[i] == '`' {
					n := runLength(line, i, '`')
					i += n
					if n == inline {
						inline = 0
					}
					continue
				}
				i++
				continue
			}
			if strings.HasPrefix(line[i:], "<!--") {
				comment = true
				i += 4
				continue
			}
			if line[i] == '\\' {
				i += 2
				continue
			}
			if line[i] == '`' {
				n := runLength(line, i, '`')
				if hasClosingTicks(content[lineStart+i+n:], n) {
					inline = n
				}
				i += n
				continue
			}
			if !strings.HasPrefix(line[i:], "[[") {
				i++
				continue
			}
			end := strings.Index(line[i+2:], "]]")
			if end < 0 {
				i += 2
				continue
			}
			raw := line[i+2 : i+2+end]
			if strings.Contains(raw, "[[") {
				i += 2
				continue
			}
			target, alias, _ := strings.Cut(raw, "|")
			target, fragment, _ := strings.Cut(target, "#")
			link := Link{Target: strings.TrimSpace(target), Fragment: strings.TrimSpace(fragment), Alias: strings.TrimSpace(alias), Line: lineNumber + 1, Embed: i > 0 && line[i-1] == '!' && !escaped(line, i-1)}
			if link.Target != "" || link.Fragment != "" {
				links = append(links, link)
			}
			i += end + 4
		}
	}
	return links
}

func hasClosingTicks(s string, length int) bool {
	for i := 0; i < len(s); {
		if s[i] != '`' {
			i++
			continue
		}
		n := runLength(s, i, '`')
		if n == length {
			return true
		}
		i += n
	}
	return false
}

func runLength(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}

func escaped(s string, i int) bool {
	n := 0
	for i > 0 && s[i-1] == '\\' {
		n++
		i--
	}
	return n%2 != 0
}

// Links reads a note and parses its outgoing wiki links without an index.
func (v *Vault) Links(name string) ([]Link, error) {
	content, err := v.Read(name)
	if err != nil {
		return nil, err
	}
	return ParseLinks(string(content)), nil
}
