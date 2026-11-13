package retrieval

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/andrewmarous/trailmix/vault"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

const passageFormat = "markdown-v2-sections-tokens-2032"

type passage struct {
	Path                                                string
	Line, EndLine, Start, End, SectionStart, SectionEnd int
	Text, Input, Hash, SourceHash                       string
}
type sourceNote struct{ Path, Body, Hash string }
type sourceRange struct{ Start, End int }
type section struct {
	sourceRange
	Headings []string
	Blocks   []int
}

func hash(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func collect(v *vault.Vault) ([]sourceNote, string, error) {
	info, err := os.Lstat(v.Root)
	if err != nil {
		return nil, "", err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, "", fmt.Errorf("vault must be a directory, not a symlink")
	}
	var notes []sourceNote
	digest := sha256.New()
	err = filepath.WalkDir(v.Root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == v.Root {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || d.Type()&os.ModeSymlink != 0 || !strings.EqualFold(filepath.Ext(path), ".md") {
			return nil
		}
		info, e := d.Info()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("note is not a regular file: %s", path)
		}
		if info.Size() > 4*1024*1024 {
			return fmt.Errorf("note exceeds 4 MiB indexing limit: %s", path)
		}
		rel, e := filepath.Rel(v.Root, path)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		body, e := v.Read(rel)
		if e != nil {
			return e
		}
		if len(body) > 4*1024*1024 {
			return fmt.Errorf("note exceeds 4 MiB indexing limit: %s", rel)
		}
		if !utf8.Valid(body) {
			return fmt.Errorf("note is not UTF-8: %s", rel)
		}
		h := hash(body)
		fmt.Fprintf(digest, "%d:%s:%s\n", len(rel), rel, h)
		notes = append(notes, sourceNote{rel, string(body), h})
		return nil
	})
	return notes, fmt.Sprintf("%x", digest.Sum(nil)), err
}

// Goldmark omits delimiters (notably fences) from block line segments. Capture
// opening positions in the parser rather than reconstructing Markdown syntax.
type locatedParser struct{ parser.BlockParser }

func (p locatedParser) Open(parent ast.Node, r text.Reader, pc parser.Context) (ast.Node, parser.State) {
	_, pos := r.Position()
	node, state := p.BlockParser.Open(parent, r, pc)
	if node != nil {
		node.SetAttributeString("source-start", pos.Start)
	}
	return node, state
}

func blockStart(n ast.Node, body []byte) int {
	start := len(body)
	ast.Walk(n, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if v, ok := n.AttributeString("source-start"); ok {
			start = min(start, v.(int))
		}
		if n.Type() != ast.TypeInline {
			for i := 0; i < n.Lines().Len(); i++ {
				start = min(start, n.Lines().At(i).Start)
			}
		}
		if t, ok := n.(*ast.Text); ok {
			start = min(start, t.Segment.Start)
		}
		return ast.WalkContinue, nil
	})
	return bytes.LastIndexByte(body[:start], '\n') + 1
}

func sections(body string) (string, []section) {
	blocks := parser.DefaultBlockParsers()
	for i, b := range blocks {
		blocks[i] = util.Prioritized(locatedParser{b.Value.(parser.BlockParser)}, b.Priority)
	}
	md := goldmark.New(goldmark.WithParser(parser.NewParser(parser.WithBlockParsers(blocks...))), goldmark.WithExtensions(extension.GFM))
	source := []byte(body)
	doc := md.Parser().Parse(text.NewReader(source))
	title := ""
	var result []section
	var stack []*ast.Heading
	current := section{sourceRange: sourceRange{0, len(body)}}
	for n := doc.FirstChild(); n != nil; n = n.NextSibling() {
		start := blockStart(n, source)
		if h, ok := n.(*ast.Heading); ok {
			current.End = start
			if len(current.Blocks) > 0 {
				result = append(result, current)
			}
			for len(stack) > 0 && stack[len(stack)-1].Level >= h.Level {
				stack = stack[:len(stack)-1]
			}
			stack = append(stack, h)
			headings := make([]string, len(stack))
			for i, h := range stack {
				headings[i] = string(h.Text(source))
			}
			if title == "" && h.Level == 1 {
				title = headings[len(headings)-1]
			}
			current = section{sourceRange: sourceRange{start, len(body)}, Headings: headings}
		} else {
			current.Blocks = append(current.Blocks, start)
		}
	}
	if len(current.Blocks) > 0 {
		result = append(result, current)
	}
	return title, result
}

func retrievalText(path, title string, headings []string, body string) string {
	if title == "" {
		title = path
	}
	return "Note: " + title + "\nPath: " + path + "\nSection: " + strings.Join(headings, " > ") + "\n\n" + body
}

// fit partitions source ranges: all emitted bytes belong to exactly one passage.
// Complete blocks are preferred; an oversized block falls back to line/sentence
// boundaries, then UTF-8 boundaries. Every accepted candidate is token-checked.
func fit(ctx context.Context, body string, s section, input func(string) string, fits func(context.Context, string) (bool, error)) ([]sourceRange, error) {
	check := func(start, end int) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		return fits(ctx, input(body[start:end]))
	}
	if ok, err := check(s.Start, s.End); err != nil || ok {
		if err != nil {
			return nil, err
		}
		return []sourceRange{s.sourceRange}, nil
	}
	if ok, err := fits(ctx, input("")); err != nil || !ok {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("note title and heading context exceed embedding token budget")
	}
	boundaries := append(append([]int(nil), s.Blocks[1:]...), s.End)
	var runeEnds []int
	var result []sourceRange
	for start := s.Start; start < s.End; {
		end := start
		for _, b := range boundaries[sort.SearchInts(boundaries, start+1):] {
			ok, err := check(start, b)
			if err != nil {
				return nil, err
			}
			if !ok {
				break
			}
			end = b
		}
		if end == start {
			// Bisect rune boundaries; each selected prefix is checked independently.
			if runeEnds == nil {
				for i := s.Start; i < s.End; {
					_, size := utf8.DecodeRuneInString(body[i:])
					i += size
					runeEnds = append(runeEnds, i)
				}
			}
			first := sort.SearchInts(runeEnds, start+1)
			blockEnd := boundaries[sort.SearchInts(boundaries, start+1)]
			lo, hi := first, sort.SearchInts(runeEnds, blockEnd)+1
			for lo < hi {
				mid := (lo + hi) / 2
				ok, err := check(start, runeEnds[mid])
				if err != nil {
					return nil, err
				}
				if ok {
					lo = mid + 1
				} else {
					hi = mid
				}
			}
			if lo == first {
				return nil, fmt.Errorf("no source text fits embedding token budget")
			}
			end = runeEnds[lo-1]
			if at := strings.LastIndexAny(body[start:end], "\n.!?。！？"); at >= 0 {
				candidate := start + at
				_, size := utf8.DecodeRuneInString(body[candidate:])
				candidate += size
				ok, err := check(start, candidate)
				if err != nil {
					return nil, err
				}
				if ok && candidate > s.Blocks[0] {
					end = candidate
				}
			}
		}
		result = append(result, sourceRange{start, end})
		start = end
	}
	return result, nil
}

func splitNote(ctx context.Context, n sourceNote, fits func(context.Context, string) (bool, error)) ([]passage, error) {
	title, parts := sections(n.Body)
	var result []passage
	for _, s := range parts {
		input := func(body string) string { return retrievalText(n.Path, title, s.Headings, body) }
		ranges, err := fit(ctx, n.Body, s, input, fits)
		if err != nil {
			return nil, fmt.Errorf("chunk %s: %w", n.Path, err)
		}
		for _, r := range ranges {
			raw := n.Body[r.Start:r.End]
			if strings.TrimSpace(raw) == "" {
				continue
			}
			in := input(raw)
			line := 1 + strings.Count(n.Body[:r.Start], "\n")
			endLine := line + strings.Count(strings.TrimSuffix(raw, "\n"), "\n")
			result = append(result, passage{Path: n.Path, Line: line, EndLine: endLine, Start: r.Start, End: r.End, Text: raw, Input: in, Hash: hash([]byte(in)), SourceHash: n.Hash, SectionStart: s.Start, SectionEnd: s.End})
		}
	}
	return result, nil
}
