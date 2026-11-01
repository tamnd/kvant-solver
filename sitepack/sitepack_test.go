package sitepack

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, root, name, body string) {
	t.Helper()
	full := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEveryFileComesBackOutOfItsPack(t *testing.T) {
	root := t.TempDir()
	files := map[string]string{
		"index.html":                    "<h1>home</h1>",
		"ru/1975/04/index.html":         "<h1>issue</h1>",
		"ru/1975/04/02_zadachi.html":    "<h1>Задачи</h1>",
		"ru/1975/10/pages.html":         "<p>sheets</p>",
		"assets/site.css":               "body{}",
		"assets/fonts/KaTeX_Main.woff2": "\x00\x01binary",
		"404.html":                      "missing",
	}
	for n, b := range files {
		write(t, root, n, b)
	}
	write(t, root, "_headers", "/*\n")
	res, err := Pack(root)
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != len(files) {
		t.Fatalf("packed %d files, want %d", res.Files, len(files))
	}
	index := map[string]Entry{}
	for _, key := range res.Manifest.Shards {
		var raw map[string][]any
		if err := json.Unmarshal(res.Objects[key].Data, &raw); err != nil {
			t.Fatal(err)
		}
		for k, v := range raw {
			index[k] = Entry{Pack: v[0].(string), Offset: int64(v[1].(float64)), Length: int64(v[2].(float64)), Type: v[3].(string), Gzip: v[5].(float64) == 1}
			if Shard(k) != indexOf(res.Manifest.Shards, key) {
				t.Errorf("%q is in the wrong shard", k)
			}
		}
	}
	for name, want := range files {
		e, ok := index[Key(name)]
		if !ok {
			t.Fatalf("%s (%q) is not in the index", name, Key(name))
		}
		data := res.Objects[e.Pack].Data[e.Offset : e.Offset+e.Length]
		if e.Gzip {
			zr, err := gzip.NewReader(bytes.NewReader(data))
			if err != nil {
				t.Fatal(err)
			}
			data, _ = io.ReadAll(zr)
		}
		if string(data) != want {
			t.Errorf("%s came back as %q", name, data)
		}
	}
	if _, ok := index["_headers"]; ok {
		t.Error("the Pages headers file was packed")
	}
	for _, k := range []string{"", "ru/1975/04/", "ru/1975/04/02_zadachi", "404"} {
		if _, ok := index[k]; !ok {
			t.Errorf("no address %q", k)
		}
	}
}

func TestAPackIsNamedByItsContent(t *testing.T) {
	root := t.TempDir()
	write(t, root, "ru/1975/04/a.html", "a")
	write(t, root, "en/1975/04/a.html", "b")
	first, _ := Pack(root)
	write(t, root, "en/1975/04/a.html", "c")
	second, _ := Pack(root)
	same := 0
	for k := range first.Objects {
		if _, ok := second.Objects[k]; ok && k[:5] == "packs" {
			same++
		}
	}
	if same != 1 {
		t.Fatalf("%d packs survived a change to one language, want the other one", same)
	}
	remove := second.Retire(&first.Manifest)
	if len(remove) != 0 || len(second.Manifest.Retired) == 0 {
		t.Fatalf("retired %v, removed %v", second.Manifest.Retired, remove)
	}
}

func indexOf(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}
