package main

import (
	"fmt"
	"net/http"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/publish"
)

// runServe serves a built site for a look before it is deployed.
func runServe(args []string) error {
	fs := pflag.NewFlagSet("serve", pflag.ContinueOnError)
	dir := fs.String("dir", "site", "the built site")
	addr := fs.String("addr", "localhost:8080", "where to listen")
	if err := fs.Parse(args); err != nil {
		return err
	}
	fmt.Printf("serving %s at http://%s/\n", *dir, *addr)
	return http.ListenAndServe(*addr, publish.Handler(*dir)) //nolint:gosec // a local preview
}
