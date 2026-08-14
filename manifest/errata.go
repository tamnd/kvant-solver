package manifest

import (
	"cmp"
	"slices"
)

// Errata is every disagreement between sources that could not be resolved
// silently. The rule the project works to is that a disagreement is either
// resolved by evidence or written down here, and never averaged away. Fifty
// years of two independently typed tables of contents will disagree, and the
// disagreements are themselves a finding about the archive.
type Errata struct {
	Count   int       `yaml:"count"`
	Entries []Erratum `yaml:"entries"`
}

// Erratum is one disagreement.
type Erratum struct {
	Issue string `yaml:"issue"`

	// Kind says what the disagreement is about, so that the file can be read
	// by category: page, title, authors, missing_row, number.
	Kind string `yaml:"kind"`

	// Subject is the thing in dispute, usually an article title, so that a
	// person reading the file knows what it is about without opening the scan.
	Subject string `yaml:"subject,omitempty"`

	// Claims is what each source said, keyed by source name.
	Claims map[string]string `yaml:"claims"`

	// Resolution is which claim was taken and why. It is empty while the
	// disagreement is still open, and an open erratum is not an error: it is
	// work for a person.
	Resolution string `yaml:"resolution,omitempty"`
	Taken      string `yaml:"taken,omitempty"`
}

// Add records a disagreement, replacing an earlier one about the same issue,
// kind and subject so that a resync does not pile up duplicates.
func (e *Errata) Add(entry Erratum) {
	if i := slices.IndexFunc(e.Entries, sameAs(entry)); i >= 0 {
		// A resolution written by a person survives a resync. Losing one
		// because a sync ran again is the one failure this file cannot have.
		if entry.Resolution == "" {
			entry.Resolution = e.Entries[i].Resolution
			entry.Taken = e.Entries[i].Taken
		}
		e.Entries[i] = entry
		return
	}
	e.Entries = append(e.Entries, entry)
}

// sameAs matches the entry an earlier run wrote about the same thing.
func sameAs(entry Erratum) func(Erratum) bool {
	return func(x Erratum) bool {
		return x.Issue == entry.Issue && x.Kind == entry.Kind && x.Subject == entry.Subject
	}
}

// Carry folds the errata file of an earlier run into this one.
//
// The entries belong to the run that has just finished, so a disagreement that
// has since been fixed leaves the file instead of living in it forever. Two
// things survive. A resolution someone wrote by hand, which is the one thing
// this file cannot lose. And any entry about a part of the archive this run did
// not look at, because a run over one year has nothing to say about the other
// fifty six, and dropping those would turn a partial sync into a claim that the
// rest of the archive is clean.
func (e *Errata) Carry(old *Errata, lookedAt func(Erratum) bool) {
	for _, x := range old.Entries {
		i := slices.IndexFunc(e.Entries, sameAs(x))
		switch {
		case i >= 0:
			if e.Entries[i].Resolution == "" {
				e.Entries[i].Resolution = x.Resolution
				e.Entries[i].Taken = x.Taken
			}
		case !lookedAt(x):
			e.Entries = append(e.Entries, x)
		}
	}
}

// Open returns the entries nobody has resolved yet.
func (e *Errata) Open() []Erratum {
	var out []Erratum
	for _, x := range e.Entries {
		if x.Resolution == "" {
			out = append(out, x)
		}
	}
	return out
}

// Sort puts the file in issue order.
func (e *Errata) Sort() {
	slices.SortFunc(e.Entries, func(a, b Erratum) int {
		if c := compareKeys(a.Issue, b.Issue); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		return cmp.Compare(a.Subject, b.Subject)
	})
	e.Count = len(e.Entries)
}
