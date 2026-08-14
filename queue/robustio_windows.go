//go:build windows

package queue

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// Windows lets a file be held in a way that stops anybody else renaming or
// deleting it, and the virus scanner and the search indexer both open a file the
// moment it appears. A rename that would work on Unix comes back as a sharing
// violation for a few milliseconds and then works. The Go toolchain carries the
// same retry for the same reason, and without it this queue hands the same page
// out twice on a machine that is only busy.
const (
	retryFor   = 2 * time.Second
	retryEvery = 5 * time.Millisecond
)

// errSharingViolation is ERROR_SHARING_VIOLATION, which package syscall does not
// name. It is the one this package sees most, since it is what a rename gets
// while somebody else has the file open.
const errSharingViolation = syscall.Errno(32)

func retry(op func() error) error {
	for waited := time.Duration(0); ; waited += retryEvery {
		err := op()
		if err == nil || !transient(err) || waited >= retryFor {
			return err
		}
		time.Sleep(retryEvery)
	}
}

// transient is somebody else holding the file. A missing file is an answer and
// not a wait, and so is everything else, so only these two are worth sleeping
// on.
func transient(err error) bool {
	return errors.Is(err, errSharingViolation) ||
		errors.Is(err, syscall.ERROR_ACCESS_DENIED)
}

func renameFile(from, to string) error { return retry(func() error { return os.Rename(from, to) }) }
func removeFile(name string) error     { return retry(func() error { return os.Remove(name) }) }
func linkFile(from, to string) error   { return retry(func() error { return os.Link(from, to) }) }

func readFile(name string) ([]byte, error) {
	var raw []byte
	err := retry(func() error {
		var err error
		raw, err = os.ReadFile(name)
		return err
	})
	return raw, err
}
