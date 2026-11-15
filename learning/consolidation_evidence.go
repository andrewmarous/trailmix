package learning

import "fmt"

func verifyEvidenceHashes(r *Registry, hashes map[string]string) error {
	read := evidenceReader(r)
	for uri, hash := range hashes {
		ref, err := parseCitation(uri)
		if err != nil {
			return err
		}
		turns, err := read(ref)
		if err != nil {
			return err
		}
		if evidenceHash(turns) != hash {
			return fmt.Errorf("consolidation evidence changed: %s", uri)
		}
	}
	return nil
}

func citationSet(refs []sessionCitation) map[string]bool {
	set := map[string]bool{}
	for _, ref := range refs {
		for _, n := range ref.Turns {
			set[evidenceURI(ref.SessionID, []int{n})] = true
		}
	}
	return set
}

// A merge may move provenance to another changed note, but cannot silently
// remove it. Legacy prose can be replaced with equivalent machine citations.
func preserveConsolidationCitations(changes []Change, notes map[string]string) error {
	var old, newRefs []sessionCitation
	for _, ch := range changes {
		refs, err := markdownCitations(notes[ch.Path])
		if err != nil {
			return err
		}
		old = append(old, refs...)
		old = append(old, legacyCitations(notes[ch.Path])...)
		refs, err = markdownCitations(ch.Content)
		if err != nil {
			return err
		}
		newRefs = append(newRefs, refs...)
	}
	present := citationSet(newRefs)
	for uri := range citationSet(old) {
		if !present[uri] {
			return fmt.Errorf("consolidation drops provenance: %s", uri)
		}
	}
	return nil
}

func validateConsolidationEvidence(r *Registry, changes []Change, hashes map[string]string) error {
	var available []sessionCitation
	for uri := range hashes {
		ref, err := parseCitation(uri)
		if err != nil {
			return err
		}
		available = append(available, ref)
	}
	supplied := citationSet(available)
	for _, ch := range changes {
		refs, err := markdownCitations(ch.Content)
		if err != nil {
			return err
		}
		for uri := range citationSet(refs) {
			if !supplied[uri] {
				return fmt.Errorf("%s: evidence was not supplied in the read bundle: %s", ch.Path, uri)
			}
		}
	}
	return validateEvidenceReferences(r, changes)
}
