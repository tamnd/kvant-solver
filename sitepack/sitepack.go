// Package sitepack packs a built site into a few large objects and an index,
// so that it can be served out of R2 by a Worker with ranged reads.
//
// A static host that stores one object per page caps the number of pages, and
// the site is tens of thousands of them. Packed, it is a few hundred objects:
// one or a few per language and year, since that is what a translation pass or
// a reread rewrites together, so a deploy uploads what changed and nothing
// else. Every object is named by its content, so a deploy never overwrites
// anything a reader may be in the middle of fetching; the one fixed name is
// the manifest, written last, which is what makes a deploy atomic.
//
// The index is split into shards by a hash of the address, so a Worker that
// has just started reads one shard of a few kilobytes rather than an index of
// the whole site. Text is stored gzipped, which is what nearly every browser
// accepts as it is, and an entry carries the hash of its content to answer
// conditional requests with a 304.
package sitepack

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"mime"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Shards is how many pieces the index is cut into. The Worker computes the
// same hash, so this is part of the format.
const Shards = 256

// ManifestKey is the one object with a fixed name.
const ManifestKey = "site.json"

// MaxPack is where a group is cut into another pack, kept well under what one
// upload through the API takes.
const MaxPack = 48 << 20

// Entry is where one address lives: pack, offset, length, content type, the
// hash of the content as it was written, and whether the stored bytes are
// gzipped. It is a JSON array in the shard to keep the index small.
type Entry struct {
	Pack   string
	Offset int64
	Length int64
	Type   string
	ETag   string
	Gzip   bool
}

func (e Entry) MarshalJSON() ([]byte, error) {
	gz := 0
	if e.Gzip {
		gz = 1
	}
	return json.Marshal([]any{e.Pack, e.Offset, e.Length, e.Type, e.ETag, gz})
}

// Manifest is the root of a deploy.
type Manifest struct {
	Version int      `json:"version"`
	Build   string   `json:"build"`
	Shards  []string `json:"shards"`
	Packs   []string `json:"packs"`
	// Retired is what the previous deploy used and this one does not. It is
	// deleted one deploy later, so a reader holding the old manifest still
	// finds what it points at.
	Retired []string `json:"retired,omitempty"`
}

// Object is one thing to upload.
type Object struct {
	Key  string
	Type string
	Data []byte
}

// Result is a packed site.
type Result struct {
	Manifest Manifest
	// Objects is every pack and shard, by key. The manifest is not among
	// them, since it has to go last.
	Objects map[string]Object
	Files   int
	Raw     int64
	Stored  int64
}

// Key is the address a file is served at: a page without its .html, a
// directory's index as the directory, anything else as it is.
func Key(name string) string {
	switch {
	case name == "index.html":
		return ""
	case strings.HasSuffix(name, "/index.html"):
		return strings.TrimSuffix(name, "index.html")
	case strings.HasSuffix(name, ".html"):
		return strings.TrimSuffix(name, ".html")
	}
	return name
}

// Shard is which index shard an address is in: FNV-1a over its bytes.
func Shard(key string) int {
	h := uint32(2166136261)
	for i := 0; i < len(key); i++ {
		h ^= uint32(key[i])
		h *= 16777619
	}
	return int(h % Shards)
}

// group is which pack a file goes in: a language and a year together, a
// language's section together, everything else by its first directory.
func group(name string) string {
	segs := strings.Split(name, "/")
	switch {
	case len(segs) >= 3:
		return segs[0] + "/" + segs[1]
	case len(segs) == 2:
		return segs[0]
	}
	return "_root"
}

// compressible is the text worth gzipping.
func compressible(ctype string) bool {
	for _, p := range []string{"text/", "application/json", "application/javascript", "image/svg+xml", "application/xml"} {
		if strings.HasPrefix(ctype, p) {
			return true
		}
	}
	return false
}

func contentType(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".json":
		return "application/json; charset=utf-8"
	case ".xml":
		return "application/xml; charset=utf-8"
	case ".txt", "":
		return "text/plain; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".woff2":
		return "font/woff2"
	}
	if t := mime.TypeByExtension(path.Ext(name)); t != "" {
		return t
	}
	return "application/octet-stream"
}

func hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

type file struct {
	name, key, ctype, etag string
	data                   []byte
	gz                     bool
	raw                    int64
}

// Pack reads the site under root and packs it.
func Pack(root string) (*Result, error) {
	groups := map[string][]file{}
	res := &Result{Objects: map[string]Object{}}
	err := filepath.WalkDir(root, func(full string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(root, full)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if name == "_headers" || name == "_redirects" {
			return nil // the Worker sets these itself
		}
		data, err := os.ReadFile(full) //nolint:gosec // under root
		if err != nil {
			return err
		}
		f := file{name: name, key: Key(name), ctype: contentType(name), etag: hash(data)[:16], raw: int64(len(data))}
		f.data = data
		if compressible(f.ctype) {
			var b bytes.Buffer
			zw, _ := gzip.NewWriterLevel(&b, gzip.BestCompression)
			_, _ = zw.Write(data)
			_ = zw.Close()
			if b.Len() < len(data) {
				f.data, f.gz = b.Bytes(), true
			}
		}
		groups[group(name)] = append(groups[group(name)], f)
		res.Files++
		res.Raw += f.raw
		res.Stored += int64(len(f.data))
		return nil
	})
	if err != nil {
		return nil, err
	}

	shards := make([]map[string]Entry, Shards)
	for i := range shards {
		shards[i] = map[string]Entry{}
	}
	names := make([]string, 0, len(groups))
	for g := range groups {
		names = append(names, g)
	}
	sort.Strings(names)
	var packs []string
	for _, g := range names {
		files := groups[g]
		sort.Slice(files, func(i, j int) bool { return files[i].name < files[j].name })
		for start := 0; start < len(files); {
			var buf bytes.Buffer
			end := start
			for end < len(files) && (end == start || buf.Len()+len(files[end].data) <= MaxPack) {
				buf.Write(files[end].data)
				end++
			}
			body := buf.Bytes()
			key := "packs/" + hash(body)[:24] + ".pack"
			res.Objects[key] = Object{Key: key, Type: "application/octet-stream", Data: body}
			packs = append(packs, key)
			var off int64
			for _, f := range files[start:end] {
				shards[Shard(f.key)][f.key] = Entry{Pack: key, Offset: off, Length: int64(len(f.data)), Type: f.ctype, ETag: f.etag, Gzip: f.gz}
				off += int64(len(f.data))
			}
			start = end
		}
	}

	res.Manifest = Manifest{Version: 1, Packs: packs, Shards: make([]string, Shards)}
	all := sha256.New()
	for i, entries := range shards {
		body, err := json.Marshal(entries)
		if err != nil {
			return nil, err
		}
		key := "idx/" + hash(body)[:24] + ".json"
		res.Objects[key] = Object{Key: key, Type: "application/json", Data: body}
		res.Manifest.Shards[i] = key
		all.Write([]byte(key))
	}
	for _, p := range packs {
		all.Write([]byte(p))
	}
	res.Manifest.Build = hex.EncodeToString(all.Sum(nil))[:16]
	return res, nil
}

// Retire works out what the previous manifest used that this one does not,
// and what is now safe to delete: what the previous deploy had retired and
// this one has not taken back.
func (r *Result) Retire(prev *Manifest) (remove []string) {
	if prev == nil {
		return nil
	}
	used := map[string]bool{}
	for k := range r.Objects {
		used[k] = true
	}
	for _, k := range append(append([]string{}, prev.Packs...), prev.Shards...) {
		if !used[k] {
			r.Manifest.Retired = append(r.Manifest.Retired, k)
		}
	}
	sort.Strings(r.Manifest.Retired)
	stillRetired := map[string]bool{}
	for _, k := range r.Manifest.Retired {
		stillRetired[k] = true
	}
	for _, k := range prev.Retired {
		if !used[k] && !stillRetired[k] {
			remove = append(remove, k)
		}
	}
	return remove
}
