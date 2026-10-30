package publish

import (
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Handler serves a built site the way both hosts do: an address with no
// extension is the page of that name with .html after it, a directory is its
// index, and anything else is the page for a missing address with a 404.
//
// It is what kvant serve runs, so that what a reader will see can be looked at
// before anything is uploaded. The standard file server is not enough on its
// own, because it serves a directory listing where the hosts serve an index
// and knows nothing of extensionless pages, so every link on the site would
// break under it.
func Handler(root string) http.Handler {
	files := http.FileServer(http.Dir(root))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := path.Clean("/" + r.URL.Path)
		full := filepath.Join(root, filepath.FromSlash(name))
		if info, err := os.Stat(full); err == nil {
			if !info.IsDir() || exists(filepath.Join(full, "index.html")) {
				if info.IsDir() && !strings.HasSuffix(r.URL.Path, "/") {
					http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
					return
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		if exists(full + ".html") {
			http.ServeFile(w, r, full+".html")
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		if data, err := os.ReadFile(filepath.Join(root, "404.html")); err == nil { //nolint:gosec // inside root
			_, _ = w.Write(data)
		}
	})
}

func exists(name string) bool {
	_, err := os.Stat(name)
	return err == nil
}
