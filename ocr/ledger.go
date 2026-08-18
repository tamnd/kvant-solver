package ocr

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tamnd/kvant-solver/api"
	"github.com/tamnd/kvant-solver/corpus"
)

// Ledger is one line per page read, appended as the run goes.
//
// The milestone asks for a cost and token report per year, and there is nowhere
// else that could come from. The queue keeps a job until it is done and then
// keeps the outcome, but it does not keep what the answer cost, and a summary
// printed at the end of a run is gone when the terminal closes. Twenty years is
// several days of running across several machines, so the record has to be a
// file that each run appends to and nothing rewrites.
//
// JSON lines rather than a table, because a run that is killed mid write should
// cost the report one line and not the file. Each line is complete on its own.
type Ledger struct {
	Path string

	mu   sync.Mutex
	file *os.File
}

// Entry is one page, whether or not it was accepted.
//
// The rejected ones matter as much as the accepted ones: a page that was read
// three times and thrown away three times cost three times what a page that
// worked cost, and a cost report that counts only the corpus understates the
// run by exactly the amount that is worth knowing about.
type Entry struct {
	TS      time.Time `json:"ts"`
	Target  string    `json:"target"`
	Issue   string    `json:"issue"`
	Year    int       `json:"year"`
	Engine  string    `json:"engine"`
	Host    string    `json:"host,omitempty"`
	Seconds float64   `json:"seconds"`
	// Usage is zero for a lane that reports nothing, which is every lane that
	// is a program on a box rather than an endpoint. That is recorded as zero
	// and read back as unknown, and the report says which is which by counting
	// the pages that came with no numbers at all.
	Usage  api.Usage `json:"usage"`
	OK     bool      `json:"ok"`
	Reason string    `json:"reason,omitempty"`
}

// PageYear is the year of the page this line is about.
//
// It is read off the target rather than taken from the field, because the
// field was written from the runner rather than from the page until recently
// and a ledger is never rewritten. A repair run leases from a queue that spans
// issues, so those lines file a page of 1980 under whichever issue the runner
// was opened on, and a report that went by the field would move a year of work
// onto the wrong row. There are 791 of them in the archive on the box that has
// read the Soviet decades so far.
//
// Reading it off the target heals the lines already written as well as the
// ones written since, which a fix in the writer alone cannot do.
func (e Entry) PageYear() int {
	if id, err := corpus.ParsePageID(e.Target); err == nil {
		return id.Issue.Year
	}
	return e.Year
}

// OpenLedger opens a ledger for appending, making its directory if it has to.
func OpenLedger(path string) (*Ledger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	return &Ledger{Path: path, file: file}, nil
}

// Append writes one line and flushes it.
//
// Flushed every time on purpose. A buffered ledger loses the last few pages of
// exactly the run that was killed, which is the run somebody is trying to
// account for. One page takes seconds, so a write between them costs nothing
// worth measuring.
func (l *Ledger) Append(entry Entry) error {
	if l == nil || l.file == nil {
		return nil
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	_, err = l.file.Write(append(raw, '\n'))
	return err
}

// Close closes the file. A nil ledger closes fine, so a run with no accounting
// does not have to guard the call.
func (l *Ledger) Close() error {
	if l == nil || l.file == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	err := l.file.Close()
	l.file = nil
	return err
}

// ReadLedger loads every entry a ledger holds.
//
// A line that does not parse is skipped rather than fatal. The one way this
// file gets damaged is a machine dying mid write, and that costs the last line;
// refusing to report anything because of it would be the wrong trade for a
// record whose whole purpose is to survive interruption.
func ReadLedger(path string) ([]Entry, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = file.Close() }()

	var out []Entry
	scanner := bufio.NewScanner(file)
	// A page answer never lands here, but a reason might be long, so the line
	// limit is raised well above the default 64k to be sure of it.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry Entry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		out = append(out, entry)
	}
	return out, scanner.Err()
}
