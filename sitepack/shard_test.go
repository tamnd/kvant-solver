package sitepack

import "testing"

// The Worker hashes the same way; these are the values worker/site.js gives.
func TestShardMatchesTheWorker(t *testing.T) {
	for key, want := range map[string]int{"": 197, "ru/1975/04/": 73, "ru/1975/04/02_zadachi": 120, "vi/problems/": 254, "assets/site.css": 231, "zh/作者": 8} {
		if got := Shard(key); got != want {
			t.Errorf("Shard(%q) = %d, the Worker says %d", key, got, want)
		}
	}
}
