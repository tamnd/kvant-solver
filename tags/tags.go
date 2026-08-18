// Package tags is the permanent name of every object in the corpus.
//
// The convention is the Stacks Project's, lifted whole. Four characters, never
// reused, never edited, and recorded in one append only file. The reason to
// want it here is that everything else about an object can move. An article's
// slug comes from the publisher and the publisher may change it. Its file is
// named after its position in the printed contents, and a splitting bug that
// merges two articles renumbers everything under it. Its page range is derived
// from a page map that a better read of the scan can correct. A citation
// written against any of those is a citation that breaks quietly.
//
// A tag breaks loudly instead, or not at all. kvant:0A3F either resolves to one
// object or fails to resolve, and there is no third case where it resolves to
// the wrong one, because the file says what it points at and nothing rewrites
// that line.
//
// Problems are the exception that proves it. M1234 is already a permanent
// identifier in the world outside this corpus: the magazine printed it, the
// problem books reprinted it, and the olympiad literature cites it. The tag a
// problem gets is for the corpus's own cross referencing and does not replace
// the number.
package tags

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tamnd/kvant-solver/corpus"
)

// The two files, under the corpus root.
const (
	TagsFile    = "tags"
	AliasesFile = "aliases"
	Dir         = "tags"
)

// alphabet is the 36 characters a tag is written in, which is what
// corpus.ParseTag accepts. Ambiguous pairs are not excluded, because these are
// copied and pasted out of a citation rather than read off paper, and dropping
// O and I would make a tag written before the exclusion illegal.
const alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZ"

// Entry is one line of the tags file: a tag and the object it names.
type Entry struct {
	Tag   corpus.Tag
	Label string
}

// Alias is one line of the aliases file: a label an object used to have, and
// the tag it still has.
//
// A rename does not touch the tags file. The object keeps its tag, the tags
// file keeps pointing at the new label, and the old label lands here so that a
// citation written against it still lands somewhere. This is why the two files
// are separate: one is what is true now and the other is what used to be true.
type Alias struct {
	Label string
	Tag   corpus.Tag
}

// Store is the pair of files, read into memory.
type Store struct {
	dir     string
	order   []Entry
	byTag   map[corpus.Tag]string
	byLabel map[string]corpus.Tag
	aliases []Alias
}

// Open reads the tags of a corpus checkout. A corpus with no tags yet is not an
// error, it is a corpus at the start.
func Open(root string) (*Store, error) {
	s := &Store{
		dir:     filepath.Join(root, Dir),
		byTag:   map[corpus.Tag]string{},
		byLabel: map[string]corpus.Tag{},
	}
	if err := s.readTags(); err != nil {
		return nil, err
	}
	return s, s.readAliases()
}

func (s *Store) readTags() error {
	lines, err := readLines(filepath.Join(s.dir, TagsFile))
	if err != nil {
		return err
	}
	for n, line := range lines {
		tag, label, err := split(line)
		if err != nil {
			return fmt.Errorf("%s line %d: %w", TagsFile, n+1, err)
		}
		// Both directions are checked here rather than in Verify, because a
		// store that has read a broken file cannot be trusted to assign against
		// it. Verify is for the corpus and this is for the file.
		if had, ok := s.byTag[tag]; ok {
			return fmt.Errorf("%s line %d: tag %s already names %q", TagsFile, n+1, tag, had)
		}
		if had, ok := s.byLabel[label]; ok {
			return fmt.Errorf("%s line %d: %q already has tag %s", TagsFile, n+1, label, had)
		}
		s.order = append(s.order, Entry{Tag: tag, Label: label})
		s.byTag[tag], s.byLabel[label] = label, tag
	}
	return nil
}

func (s *Store) readAliases() error {
	lines, err := readLines(filepath.Join(s.dir, AliasesFile))
	if err != nil {
		return err
	}
	for n, line := range lines {
		// The columns are the other way round from the tags file, and on
		// purpose. A tags line reads "this tag names that object" and an alias
		// line reads "that old name means this tag", which is the direction
		// each is looked up in.
		label, rest, ok := strings.Cut(line, ",")
		if !ok {
			return fmt.Errorf("%s line %d: want label,TAG", AliasesFile, n+1)
		}
		tag, err := corpus.ParseTag(strings.TrimSpace(rest))
		if err != nil {
			return fmt.Errorf("%s line %d: %w", AliasesFile, n+1, err)
		}
		s.aliases = append(s.aliases, Alias{Label: strings.TrimSpace(label), Tag: tag})
	}
	return nil
}

// split reads one tags line.
func split(line string) (corpus.Tag, string, error) {
	raw, label, ok := strings.Cut(line, ",")
	if !ok {
		return "", "", fmt.Errorf("want TAG,label")
	}
	tag, err := corpus.ParseTag(strings.TrimSpace(raw))
	if err != nil {
		return "", "", err
	}
	label = strings.TrimSpace(label)
	if label == "" {
		return "", "", fmt.Errorf("tag %s names nothing", tag)
	}
	return tag, label, nil
}

// readLines returns the meaningful lines of a file, or nothing if it does not
// exist. Blank lines and # comments are skipped, so the files can carry a
// header saying what they are and what not to do to them.
func readLines(path string) ([]string, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var out []string
	scan := bufio.NewScanner(file)
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		out = append(out, line)
	}
	return out, scan.Err()
}

// Tag returns the tag an object already has.
func (s *Store) Tag(label string) (corpus.Tag, bool) {
	tag, ok := s.byLabel[label]
	return tag, ok
}

// Label returns the object a tag names, following aliases is not done here
// because an alias is a label and not a tag.
func (s *Store) Label(tag corpus.Tag) (string, bool) {
	label, ok := s.byTag[tag]
	return label, ok
}

// Resolve is Label plus the alias table, which is the lookup a citation wants:
// it is given something written down in the past and has to find the object.
func (s *Store) Resolve(what string) (corpus.Tag, string, bool) {
	if tag, err := corpus.ParseTag(what); err == nil {
		if label, ok := s.byTag[tag]; ok {
			return tag, label, true
		}
	}
	if tag, ok := s.byLabel[what]; ok {
		return tag, what, true
	}
	for _, alias := range s.aliases {
		if alias.Label != what {
			continue
		}
		if label, ok := s.byTag[alias.Tag]; ok {
			return alias.Tag, label, true
		}
	}
	return "", "", false
}

// Len is how many objects are tagged.
func (s *Store) Len() int { return len(s.order) }

// Entries returns the tags in the order the file holds them, which is the order
// they were assigned in.
func (s *Store) Entries() []Entry { return append([]Entry(nil), s.order...) }

// Aliases returns the rename table.
func (s *Store) Aliases() []Alias { return append([]Alias(nil), s.aliases...) }

// Assign gives an object a tag, or returns the one it already has.
//
// The tag is derived from the label rather than drawn in sequence or at random,
// and that is worth the collision handling it costs. A sequential tag encodes
// the order the corpus happened to be walked in, so assigning 1975 before 1974
// gives every object a different tag from assigning 1974 first, and the file
// stops being reproducible from the corpus. A derived tag does not care what
// order it is asked in.
//
// Collisions are certain and are meant to be. Four characters is 1.7 million
// tags and the corpus will hold tens of thousands, so by the birthday bound a
// few hundred pairs will want the same one. The loser of a collision is the
// object asked about second, which reintroduces order dependence for exactly
// those objects and nothing else. That is why the file is the record: what is
// written down wins, and derivation only decides what to write when there is
// nothing there yet.
func (s *Store) Assign(label string) (corpus.Tag, error) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "", fmt.Errorf("an object with no label cannot be tagged")
	}
	if strings.ContainsAny(label, ",\n") {
		return "", fmt.Errorf("label %q has a comma or a newline in it, and the file is one line per object", label)
	}
	if tag, ok := s.byLabel[label]; ok {
		return tag, nil
	}
	for probe := range 1 << 20 {
		tag := derive(label, probe)
		if _, taken := s.byTag[tag]; taken {
			continue
		}
		s.order = append(s.order, Entry{Tag: tag, Label: label})
		s.byTag[tag], s.byLabel[label] = label, tag
		return tag, nil
	}
	return "", fmt.Errorf("no tag left for %q, which means the four character space is full", label)
}

// Adopt puts a tag an object already carries into the register.
//
// It is for the one direction the register cannot recover on its own. The tags
// file can be lost, or an object can be tagged in a branch whose register was
// never merged, and in both cases the front matter of the file is the only
// surviving record of what the object was called. Deriving a fresh tag then
// would be the one thing this package promises not to do: change the permanent
// name of an object that already had one.
//
// It refuses rather than moves a tag that is already spoken for, and the caller
// falls back to Assign. Nothing is lost by that: the object never had a tag
// anybody could rely on, because two files claimed the same one.
func (s *Store) Adopt(label string, tag corpus.Tag) bool {
	label = strings.TrimSpace(label)
	if label == "" || !tag.Valid() || strings.ContainsAny(label, ",\n") {
		return false
	}
	if _, taken := s.byTag[tag]; taken {
		return false
	}
	if _, had := s.byLabel[label]; had {
		return false
	}
	s.order = append(s.order, Entry{Tag: tag, Label: label})
	s.byTag[tag], s.byLabel[label] = label, tag
	return true
}

// Rename records that an object used to be called something else.
//
// The tag does not move. That is the whole point of it: the object is the same
// object, and a citation written against the old name has to keep working.
func (s *Store) Rename(from, to string) error {
	tag, ok := s.byLabel[from]
	if !ok {
		return fmt.Errorf("%q has no tag, so there is nothing to rename", from)
	}
	if had, ok := s.byLabel[to]; ok && had != tag {
		return fmt.Errorf("%q already has tag %s, which is not %s", to, had, tag)
	}
	delete(s.byLabel, from)
	s.byLabel[to] = tag
	s.byTag[tag] = to
	for i := range s.order {
		if s.order[i].Tag == tag {
			s.order[i].Label = to
		}
	}
	for _, alias := range s.aliases {
		if alias.Label == from {
			return nil
		}
	}
	s.aliases = append(s.aliases, Alias{Label: from, Tag: tag})
	return nil
}

// derive turns a label into a tag. The probe distinguishes the attempts for one
// label, so a collision moves to a different tag rather than to the next one
// along, and two labels that collided once do not go on colliding.
func derive(label string, probe int) corpus.Tag {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%d", label, probe))
	n := binary.BigEndian.Uint64(sum[:8])
	out := make([]byte, 4)
	for i := 3; i >= 0; i-- {
		out[i] = alphabet[n%uint64(len(alphabet))]
		n /= uint64(len(alphabet))
	}
	return corpus.Tag(out)
}

// Save writes both files.
//
// The tags file is append only in meaning and rewritten in full on disk, which
// is not a contradiction: every line that was there is still there, in the same
// order, and the new ones are underneath. Writing it whole is what lets a
// rename fix a label in place, and the guarantee that matters is that a tag
// never moves and never points somewhere else.
func (s *Store) Save() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	var tags strings.Builder
	tags.WriteString("# Permanent tags, one per object, append only.\n")
	tags.WriteString("# A tag is never reused and never edited. A renamed object keeps its tag\n")
	tags.WriteString("# and leaves its old label in the aliases file next to this one.\n")
	for _, entry := range s.order {
		fmt.Fprintf(&tags, "%s,%s\n", entry.Tag, entry.Label)
	}
	if err := write(filepath.Join(s.dir, TagsFile), tags.String()); err != nil {
		return err
	}
	if len(s.aliases) == 0 {
		return nil
	}
	var aliases strings.Builder
	aliases.WriteString("# Labels objects used to have, and the tag each of them still has.\n")
	sorted := append([]Alias(nil), s.aliases...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Label < sorted[j].Label })
	for _, alias := range sorted {
		fmt.Fprintf(&aliases, "%s,%s\n", alias.Label, alias.Tag)
	}
	return write(filepath.Join(s.dir, AliasesFile), aliases.String())
}

func write(path, body string) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
