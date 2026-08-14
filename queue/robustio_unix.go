//go:build !windows

package queue

import "os"

// On Unix a rename, a read or a remove either works or fails for a reason that
// will still be there a moment later, so these are the plain calls and the
// package pays nothing for the Windows problem next door.
func renameFile(from, to string) error     { return os.Rename(from, to) }
func removeFile(name string) error         { return os.Remove(name) }
func linkFile(from, to string) error       { return os.Link(from, to) }
func readFile(name string) ([]byte, error) { return os.ReadFile(name) }
