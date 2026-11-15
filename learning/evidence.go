package learning

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Session citations name logical completed turns, not JSONL line numbers.
// The harness prefix namespaces native IDs; it does not change the URI grammar.
type sessionCitation struct {
	SessionID string
	Turns     []int
}

var sessionName = regexp.MustCompile(`^(pi|codex)/[A-Za-z0-9][A-Za-z0-9._-]*$`)
var legacySession = regexp.MustCompile(`(?:pi|codex)/[A-Za-z0-9][A-Za-z0-9._-]*`)
var legacyTurns = regexp.MustCompile(`(?i)\bturns?\s+([0-9]+(?:\s*[-–]\s*[0-9]+)?(?:\s*,\s*[0-9]+(?:\s*[-–]\s*[0-9]+)?)*)`)

func parseCitation(raw string) (sessionCitation, error) {
	var c sessionCitation
	u, err := url.Parse(raw)
	if err != nil {
		return c, fmt.Errorf("invalid evidence URI %q", raw)
	}
	c.SessionID = strings.TrimPrefix(u.Path, "/")
	if u.Scheme != "trailmix" || u.Host != "session" || u.User != nil || u.Fragment != "" || !sessionName.MatchString(c.SessionID) {
		return c, fmt.Errorf("invalid session evidence URI %q", raw)
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(q) != 1 || len(q["turns"]) != 1 {
		return c, fmt.Errorf("evidence requires one turns parameter: %q", raw)
	}
	c.Turns, err = parseTurnSet(q.Get("turns"))
	return c, err
}

func parseTurnSet(s string) ([]int, error) {
	if s == "" || len(s) > 4096 {
		return nil, fmt.Errorf("invalid evidence turn set")
	}
	seen := map[int]bool{}
	for _, part := range strings.Split(s, ",") {
		bounds := strings.Split(part, "-")
		if len(bounds) > 2 {
			return nil, fmt.Errorf("invalid evidence turn range %q", part)
		}
		first, err := strconv.Atoi(bounds[0])
		if err != nil || first < 1 {
			return nil, fmt.Errorf("invalid evidence turn %q", part)
		}
		last := first
		if len(bounds) == 2 {
			last, err = strconv.Atoi(bounds[1])
			if err != nil {
				return nil, fmt.Errorf("invalid evidence turn range %q", part)
			}
		}
		if last < first || last-first >= 1000 {
			return nil, fmt.Errorf("invalid or oversized evidence range %q", part)
		}
		for n := first; ; n++ {
			seen[n] = true
			if len(seen) > 1000 {
				return nil, fmt.Errorf("evidence exceeds 1000 turns")
			}
			if n == last {
				break
			}
		}
	}
	turns := make([]int, 0, len(seen))
	for n := range seen {
		turns = append(turns, n)
	}
	sort.Ints(turns)
	return turns, nil
}

func evidenceURI(session string, turns []int) string {
	unique := map[int]bool{}
	for _, n := range turns {
		unique[n] = true
	}
	ns := make([]int, 0, len(unique))
	for n := range unique {
		ns = append(ns, n)
	}
	sort.Ints(ns)
	var parts []string
	for i := 0; i < len(ns); i++ {
		first, last := ns[i], ns[i]
		for i+1 < len(ns) && ns[i+1] == last+1 {
			i++
			last = ns[i]
		}
		p := strconv.Itoa(first)
		if last != first {
			p += "-" + strconv.Itoa(last)
		}
		parts = append(parts, p)
	}
	return "trailmix://session/" + session + "?turns=" + strings.Join(parts, ",")
}

func markdownCitations(body string) ([]sessionCitation, error) {
	var refs []sessionCitation
	doc := goldmark.DefaultParser().Parse(text.NewReader([]byte(body)))
	err := ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		link, ok := n.(*ast.Link)
		if !entering || !ok || !strings.HasPrefix(string(link.Destination), "trailmix:") {
			return ast.WalkContinue, nil
		}
		c, err := parseCitation(string(link.Destination))
		if err != nil {
			return ast.WalkStop, err
		}
		refs = append(refs, c)
		if len(refs) > 100 {
			return ast.WalkStop, fmt.Errorf("note exceeds 100 evidence references")
		}
		return ast.WalkContinue, nil
	})
	return refs, err
}

// Legacy prose is discovery-only. New publication uses the explicit URI grammar.
func legacyCitations(body string) []sessionCitation {
	var refs []sessionCitation
	current := ""
	for _, line := range strings.Split(body, "\n") {
		if s := legacySession.FindString(line); s != "" {
			current = s
		}
		if current == "" {
			continue
		}
		for _, m := range legacyTurns.FindAllStringSubmatch(line, -1) {
			set := strings.ReplaceAll(strings.ReplaceAll(m[1], "–", "-"), " ", "")
			turns, err := parseTurnSet(set)
			if err == nil {
				refs = append(refs, sessionCitation{current, turns})
			}
		}
	}
	return refs
}

func (r *Registry) resolveEvidence(c sessionCitation) ([]Turn, error) {
	return evidenceReader(r)(c)
}

// Parse each session once per validation/read-bundle operation.
func evidenceReader(r *Registry) func(sessionCitation) ([]Turn, error) {
	cache := map[string]transcript{}
	return func(c sessionCitation) ([]Turn, error) {
		s := r.session(c.SessionID)
		if s == nil {
			return nil, fmt.Errorf("evidence session not found: %s", c.SessionID)
		}
		if s.Error != "" {
			return nil, fmt.Errorf("evidence session %s: %s", c.SessionID, s.Error)
		}
		t, ok := cache[c.SessionID]
		if !ok {
			info, err := os.Lstat(s.Path)
			if err != nil {
				return nil, fmt.Errorf("evidence session %s: %w", c.SessionID, err)
			}
			if !info.Mode().IsRegular() {
				return nil, fmt.Errorf("evidence session is not a regular file: %s", c.SessionID)
			}
			t, err = parse(s.Path, s.Harness)
			if err != nil {
				return nil, fmt.Errorf("evidence session %s: %w", c.SessionID, err)
			}
			if t.ID != s.NativeID {
				return nil, fmt.Errorf("evidence session identity changed: %s", c.SessionID)
			}
			cache[c.SessionID] = t
		}
		turns := make([]Turn, 0, len(c.Turns))
		for _, n := range c.Turns {
			if n < 1 || n > len(t.Turns) {
				return nil, fmt.Errorf("evidence %s turn %d: only %d completed turns exist", c.SessionID, n, len(t.Turns))
			}
			turns = append(turns, t.Turns[n-1])
		}
		return turns, nil
	}
}

func evidenceHash(turns []Turn) string { b, _ := json.Marshal(turns); return noteHash(string(b)) }

func validateEvidenceReferences(r *Registry, changes []Change) error {
	return validateEvidenceReferencesMode(r, changes, false)
}

// Previously saved proposals retain their prose citations and exact bodies.
// New proposals require the machine-readable URI form.
func validateEvidenceReferencesMode(r *Registry, changes []Change, allowLegacy bool) error {
	read := evidenceReader(r)
	for _, ch := range changes {
		refs, err := markdownCitations(ch.Content)
		if err != nil {
			return fmt.Errorf("%s: %w", ch.Path, err)
		}
		if allowLegacy {
			refs = append(refs, legacyCitations(ch.Content)...)
		}
		present := map[string]bool{}
		for _, ref := range refs {
			if _, err = read(ref); err != nil {
				return fmt.Errorf("%s: %w", ch.Path, err)
			}
			for _, n := range ref.Turns {
				present[evidenceURI(ref.SessionID, []int{n})] = true
			}
		}
		for _, e := range ch.Evidence {
			if e.SessionID != "" && !present[evidenceURI(e.SessionID, []int{e.Turn})] {
				return fmt.Errorf("%s: Markdown omits evidence for %s turn %d", ch.Path, e.SessionID, e.Turn)
			}
		}
	}
	return nil
}
