package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/sitepack"
)

// runDeploy packs a built site and uploads it to the bucket the Worker reads.
// The token and account come from the environment, as they do for wrangler.
func runDeploy(args []string) error {
	fs := pflag.NewFlagSet("deploy", pflag.ContinueOnError)
	dir := fs.String("dir", "site", "the built site")
	bucket := fs.String("bucket", "kvant-site", "the R2 bucket")
	workers := fs.Int("workers", 16, "uploads at once")
	dry := fs.Bool("dry-run", false, "pack and report, upload nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}
	start := time.Now()
	res, err := sitepack.Pack(*dir)
	if err != nil {
		return err
	}
	fmt.Printf("packed %d files, %.1f MB, into %d packs and %d index shards, %.1f MB stored, build %s\n",
		res.Files, float64(res.Raw)/1e6, len(res.Manifest.Packs), len(res.Manifest.Shards), float64(res.Stored)/1e6, res.Manifest.Build)
	if *dry {
		return nil
	}
	b := &sitepack.Bucket{Account: os.Getenv("CLOUDFLARE_ACCOUNT_ID"), Name: *bucket, Token: os.Getenv("CLOUDFLARE_API_TOKEN")}
	if b.Account == "" || b.Token == "" {
		return fmt.Errorf("CLOUDFLARE_ACCOUNT_ID and CLOUDFLARE_API_TOKEN must be set")
	}
	rep, err := sitepack.Deploy(context.Background(), b, res, *workers, func(f string, a ...any) { fmt.Printf(f+"\n", a...) })
	if err != nil {
		return err
	}
	fmt.Printf("deployed build %s: uploaded %d objects (%.1f MB), kept %d, removed %d, in %s\n",
		res.Manifest.Build, rep.Uploaded, float64(rep.Bytes)/1e6, rep.Kept, rep.Removed, time.Since(start).Round(time.Second))
	return nil
}
