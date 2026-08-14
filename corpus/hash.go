package corpus

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Normalise puts a body into the one form its hash is taken over.
//
// The hash is what makes the corpus maintainable, so it has to be stable
// against edits that change no content. A CRLF, a space left at the end of a
// line, a missing final newline or three blank lines where one would do are all
// noise, and a hash that moves for them would restale every translation of the
// file for nothing.
func Normalise(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")

	lines := strings.Split(body, "\n")
	out := make([]string, 0, len(lines))
	blank := 0
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if line == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, line)
	}

	joined := strings.Join(out, "\n")
	joined = strings.Trim(joined, "\n")
	if joined == "" {
		return ""
	}
	return joined + "\n"
}

// HashBody is the sha256 of the normalised body, hex encoded. It is what
// content_sha256 holds, and what a translation records about its source.
func HashBody(body string) string {
	sum := sha256.Sum256([]byte(Normalise(body)))
	return hex.EncodeToString(sum[:])
}

// HashString is the sha256 of a string taken as it stands, without
// normalisation. Prompts and glossary digests use it, because there a trailing
// space really is a different instruction.
func HashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}
