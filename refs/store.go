package refs

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/manifest"
)

// ReadAll loads every year's graph out of manifests/refs.
//
// The report is built from what is on disk rather than from a fresh pass, so
// that the document in a pull request describes the manifests in the same pull
// request. A report that quietly rebuilt would be right about the corpus and
// wrong about the files next to it.
func ReadAll(store *manifest.Store) (map[int]*Graph, error) {
	paths, err := filepath.Glob(filepath.Join(store.Dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	out := map[int]*Graph{}
	for _, path := range paths {
		name := filepath.Base(path)
		year, err := strconv.Atoi(strings.TrimSuffix(name, ".yaml"))
		if err != nil {
			// Something else living in the directory is not an error. The graphs
			// are named after years and anything that is not is not ours.
			continue
		}
		graph := &Graph{}
		if err := store.Read(name, graph); err != nil {
			return nil, err
		}
		out[year] = graph
	}
	return out, nil
}

// Read loads one year's graph.
func Read(store *manifest.Store, year int) (*Graph, error) {
	graph := &Graph{}
	if err := store.Read(strconv.Itoa(year)+".yaml", graph); err != nil {
		return nil, err
	}
	return graph, nil
}

// Lookup finds the graphs a person asked for by name.
//
// The argument is a year, or a tag, or the label of an article, because those
// are the three things somebody has in hand when they want to check a
// reference: the year they are reading, the tag a citation resolved to, or the
// identifier printed in the front matter of the file open in front of them.
func Lookup(store *manifest.Store, c *corpus.Corpus, lang, what string) ([]*Graph, error) {
	if year, err := strconv.Atoi(what); err == nil {
		graph, err := Read(store, year)
		if err != nil {
			return nil, err
		}
		return []*Graph{graph}, nil
	}

	all, err := ReadAll(store)
	if err != nil {
		return nil, err
	}
	var out []*Graph
	for _, year := range years(all) {
		var kept []Ref
		for _, ref := range all[year].Refs {
			if string(ref.From) == what || ref.FromLabel == what {
				kept = append(kept, ref)
			}
		}
		if len(kept) > 0 {
			out = append(out, &Graph{Year: year, Count: len(kept), Refs: kept})
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("nothing in manifests/refs cites anything from %q", what)
	}
	return out, nil
}

// Line is one reference as a person reads it on a terminal.
func Line(ref Ref) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-10s %-6s %s", ref.Status, ref.Kind, ref.Cite)
	switch {
	case ref.To != "":
		fmt.Fprintf(&b, " -> %s %s", ref.To, ref.ToLabel)
	case ref.Problem != "":
		fmt.Fprintf(&b, " -> %s", ref.Problem)
	case ref.Page > 0:
		fmt.Fprintf(&b, " -> %s p%d", ref.Issue, ref.Page)
	case ref.Issue != "":
		fmt.Fprintf(&b, " -> %s", ref.Issue)
	}
	if ref.Title != "" {
		fmt.Fprintf(&b, " (%s)", ref.Title)
	}
	if ref.Why != "" {
		fmt.Fprintf(&b, " [%s]", ref.Why)
	}
	return b.String()
}
