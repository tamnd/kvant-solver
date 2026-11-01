package sitepack

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"
)

// Report is what a deploy did.
type Report struct {
	Uploaded, Kept, Removed int
	Bytes                   int64
}

// Deploy uploads what the live deploy does not already have, then the
// manifest, then deletes what the deploy before last retired.
func Deploy(ctx context.Context, b *Bucket, r *Result, workers int, log func(string, ...any)) (Report, error) {
	var rep Report
	prev, err := b.Manifest(ctx)
	if err != nil {
		return rep, err
	}
	have := map[string]bool{}
	if prev != nil {
		for _, k := range append(append(append([]string{}, prev.Packs...), prev.Shards...), prev.Retired...) {
			have[k] = true
		}
	}
	remove := r.Retire(prev)

	var todo []Object
	for k, o := range r.Objects {
		if have[k] {
			rep.Kept++
			continue
		}
		todo = append(todo, o)
	}
	// Big packs first, so the slowest uploads are not the last to start.
	sort.Slice(todo, func(i, j int) bool { return len(todo[i].Data) > len(todo[j].Data) })

	jobs := make(chan Object)
	errs := make(chan error, len(todo))
	var wg sync.WaitGroup
	var mu sync.Mutex
	for range max(1, workers) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for o := range jobs {
				if err := b.Put(ctx, o.Key, o.Type, o.Data); err != nil {
					errs <- err
					continue
				}
				mu.Lock()
				rep.Uploaded++
				rep.Bytes += int64(len(o.Data))
				if rep.Uploaded%50 == 0 {
					log("uploaded %d of %d", rep.Uploaded, len(todo))
				}
				mu.Unlock()
			}
		}()
	}
	for _, o := range todo {
		jobs <- o
	}
	close(jobs)
	wg.Wait()
	close(errs)
	if err := <-errs; err != nil {
		return rep, fmt.Errorf("upload: %w (manifest left as it was)", err)
	}

	body, err := json.Marshal(r.Manifest)
	if err != nil {
		return rep, err
	}
	if err := b.Put(ctx, ManifestKey, "application/json", body); err != nil {
		return rep, err
	}
	for _, k := range remove {
		if err := b.Delete(ctx, k); err != nil {
			log("could not delete %s: %v", k, err)
			continue
		}
		rep.Removed++
	}
	return rep, nil
}
