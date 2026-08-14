package corpus

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// fence is what opens and closes the YAML block at the head of every file.
const fence = "---"

// ErrNoFrontMatter is returned for a Markdown file that has no YAML block. It
// is a distinct error because the walker treats such a file as a stray rather
// than as a corrupt corpus file.
var ErrNoFrontMatter = errors.New("file has no front matter")

// Split separates the YAML block from the body. It does not parse either.
func Split(data []byte) (front, body string, err error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	if !strings.HasPrefix(text, fence+"\n") {
		return "", "", ErrNoFrontMatter
	}
	rest := text[len(fence)+1:]
	end := strings.Index(rest, "\n"+fence+"\n")
	if end < 0 {
		if strings.HasSuffix(rest, "\n"+fence) {
			return rest[:len(rest)-len(fence)-1], "", nil
		}
		return "", "", fmt.Errorf("front matter is never closed by %s", fence)
	}
	return rest[:end], strings.TrimPrefix(rest[end+len(fence)+2:], "\n"), nil
}

// Load reads a corpus file into front, and returns the body.
//
// It checks the recorded hash against the body it just read, because a file
// whose hash does not match its body is the one failure that silently poisons
// everything downstream: a stale translation looks fresh, an audit passes, and
// nobody finds out until a citation points at text that is no longer there.
func Load(path string, front Front) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	head, body, err := Split(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	dec := yaml.NewDecoder(strings.NewReader(head))
	dec.KnownFields(true)
	if err := dec.Decode(front); err != nil {
		return "", fmt.Errorf("%s: front matter: %w", path, err)
	}
	if got := HashBody(body); got != front.ContentHash() {
		return "", fmt.Errorf("%s: content_sha256 says %s but the body hashes to %s", path, short(front.ContentHash()), short(got))
	}
	return body, nil
}

// LoadUnchecked reads a file without comparing the hash. Only the writer of a
// file and the repair path have any business calling it.
func LoadUnchecked(path string, front Front) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	head, body, err := Split(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", path, err)
	}
	dec := yaml.NewDecoder(strings.NewReader(head))
	dec.KnownFields(true)
	if err := dec.Decode(front); err != nil {
		return "", fmt.Errorf("%s: front matter: %w", path, err)
	}
	return body, nil
}

// Save writes a corpus file. It normalises the body, hashes it, sets the hash
// on the front matter, validates, and only then touches the disk, so a file
// that exists is a file that was correct at the moment it was written.
//
// The write is atomic. A run that is killed part way through 16000 pages should
// leave a corpus of whole files and not a truncated one.
func Save(path string, front Front, body string) error {
	body = Normalise(body)
	front.SetContentHash(HashBody(body))
	if err := front.Validate(); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}

	var buf bytes.Buffer
	buf.WriteString(fence + "\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(front); err != nil {
		return fmt.Errorf("%s: encode front matter: %w", path, err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("%s: encode front matter: %w", path, err)
	}
	buf.WriteString(fence + "\n\n")
	buf.WriteString(body)

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".kvant-*")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func short(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}
