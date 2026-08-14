package ocr

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// A repair needs to know which conversation read the page, and that fact cannot
// go where the page goes.
//
// The conversation URL identifies a chat in somebody's ChatGPT account, and the
// profile is an absolute path in the home directory of a rented box. Both are
// the same kind of thing StripToolHeader exists to remove: machinery that says
// more about the operator than about Квант, in a repository anyone can read.
// So they are written under work/, which is gitignored, next to the raw answers
// they were taken from. Losing them costs a re-read, which is the fallback in
// any case.
//
// One file per page rather than one index per volume. Pages are filed by four
// host goroutines at once and by more than one process over a long run, and a
// shared index would need a lock that a queue this repo already has decided not
// to use anywhere else.

// Thread is where a page was read, so it can be asked about again.
type Thread struct {
	Book         string `json:"book"`
	Page         int    `json:"page"`
	Host         string `json:"host"`
	Conversation string `json:"conversation"`
	// Profile is the Chrome profile directory on that host. A conversation
	// belongs to one account, and the pool hands out a different profile per
	// lane, so the two travel together or neither is any use.
	Profile string `json:"profile"`
	Model   string `json:"model,omitempty"`
	Read    string `json:"read,omitempty"`
}

// ThreadsDir is where the conversation records live, under work/ and out of the
// corpus.
func ThreadsDir(root, book string) string {
	return filepath.Join(root, "work", "threads", book)
}

// ThreadPath is the record for one page.
func ThreadPath(root, book string, page int) string {
	return filepath.Join(ThreadsDir(root, book), fmt.Sprintf("%04d.json", page))
}

// WriteThread records where a page was read.
func WriteThread(root string, thread Thread) error {
	path := ThreadPath(root, thread.Book, thread.Page)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(thread, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o600)
}

// ReadThread returns where a page was read, or an error when there is no record
// of it. A page read before the tool reported conversation URLs has none, and
// that is not a failure, it is a page that has to go back to the image.
func ReadThread(root, book string, page int) (Thread, error) {
	raw, err := os.ReadFile(ThreadPath(root, book, page))
	if err != nil {
		return Thread{}, err
	}
	var thread Thread
	if err := json.Unmarshal(raw, &thread); err != nil {
		return Thread{}, fmt.Errorf("%s: %w", ThreadPath(root, book, page), err)
	}
	return thread, nil
}

// HeaderFields reads the block chatgpt-tool writes above an answer.
//
// It is the same block StripToolHeader throws away, and this is the one place
// anything looks inside it. The keys it carries are source, model, generated,
// elapsed, and, since the tool was taught to report them, conversation and
// profile. Nothing here requires any particular key to be present: an older
// tool writes four of them and a page read by it simply has no thread.
func HeaderFields(text string) map[string]string {
	trimmed := strings.TrimLeft(text, " \t\r\n")
	match := toolHeader.FindString(trimmed)
	if match == "" || !strings.Contains(match, "\nsource:") || !strings.Contains(match, "\nelapsed:") {
		return nil
	}
	fields := map[string]string{}
	for line := range strings.SplitSeq(match, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || line == "---" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if _, seen := fields[key]; seen {
			continue
		}
		fields[key] = strings.TrimSpace(value)
	}
	return fields
}
