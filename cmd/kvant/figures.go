package main

import (
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/pflag"

	"github.com/tamnd/kvant-solver/corpus"
	"github.com/tamnd/kvant-solver/fetch"
	"github.com/tamnd/kvant-solver/figures"
)

// runFigures cuts the figures out of the scans.
//
// A page's anchors say how many figures it has and the detector says where
// they are, and a page is only cut when the two agree. That is the harness:
// a detector that finds three pictures on a page whose text speaks of two has
// found a headline or missed a join, and a crop put under the wrong caption is
// worse than none. What disagrees is counted and left for a better detector.
func runFigures(args []string) error {
	fs := pflag.NewFlagSet("figures", pflag.ContinueOnError)
	f := addFetchFlags(fs)
	out := fs.String("out", "", "where the crops are written, cache/figures by default")
	write := fs.Bool("write", false, "write the crops and the manifests, not only count")
	verbose := fs.Bool("verbose", false, "list every page that disagrees")
	quality := fs.Int("quality", 72, "JPEG quality of the crops")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if len(*f.years) == 0 && len(*f.issues) == 0 {
		return fmt.Errorf("name an --issue or a --year")
	}
	issues, err := selected(f)
	if err != nil {
		return err
	}
	cache, err := fetch.OpenCache(*f.cache)
	if err != nil {
		return err
	}
	dir := dirOr(*out, cache.Dir, "figures")

	var total struct{ pages, anchored, agreed, more, fewer, crops, noScan int }
	for _, iss := range issues {
		idx, err := cache.ReadIndex(iss.Key)
		if err != nil {
			continue // never fetched
		}
		scan := map[int]string{}
		for _, s := range idx.Sheets {
			if s.Have() && cache.Has(s.SHA256) {
				scan[s.Ord+1] = cache.Path(s.SHA256)
			}
		}
		pages, _ := filepath.Glob(filepath.Join(*f.root, "content", corpus.DefaultLang, iss.Dir, "pages", "*.md"))
		sort.Strings(pages)
		m := &figures.Manifest{Issue: iss.Key}
		for _, p := range pages {
			index, err := strconv.Atoi(strings.TrimSuffix(filepath.Base(p), ".md"))
			if err != nil {
				continue
			}
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			_, body, err := corpus.Split(data)
			if err != nil {
				return fmt.Errorf("%s: %w", p, err)
			}
			total.pages++
			anchors := figures.Anchors(body)
			if len(anchors) == 0 {
				continue
			}
			total.anchored++
			path, ok := scan[index]
			if !ok {
				total.noScan++
				continue
			}
			img, err := decode(path)
			if err != nil {
				return fmt.Errorf("%s sheet %d: %w", iss.Key, index, err)
			}
			found := figures.Detect(img)
			switch {
			case len(found) > len(anchors):
				total.more++
			case len(found) < len(anchors):
				total.fewer++
			default:
				total.agreed++
			}
			// When they disagree the crops cannot be dealt out one to a
			// caption, and they all go to the page's first anchor instead:
			// near where they were printed, and with every caption still in
			// the text round them.
			group := len(found) != len(anchors)
			if group && *verbose {
				fmt.Printf("  %s page %d: %d anchors, %d found\n", iss.Key, index, len(anchors), len(found))
			}
			for k, fig := range found {
				a := anchors[0]
				if !group {
					a = anchors[k]
				}
				if a.Key == "" {
					continue // nothing to find it by
				}
				file := fmt.Sprintf("%s/%04d-%d.jpg", iss.Dir, index, k+1)
				e := figures.Entry{
					Page: index, N: k + 1, Number: a.Number, Key: a.Key, Group: group,
					File: file, Width: fig.Box.Dx(), Height: fig.Box.Dy(),
				}
				m.Figures = append(m.Figures, e)
				total.crops++
				if *write {
					if err := crop(img, fig.Box, filepath.Join(dir, file), *quality); err != nil {
						return err
					}
				}
			}
		}
		if *write {
			if err := m.Write(*f.root); err != nil {
				return err
			}
		}
	}
	fmt.Printf("%d pages, %d with anchors, %d without a scan here\n", total.pages, total.anchored, total.noScan)
	fmt.Printf("%d agree, %d found more, %d found fewer: %d crops\n", total.agreed, total.more, total.fewer, total.crops)
	if *write {
		fmt.Printf("crops under %s, manifests under %s\n", dir, filepath.Join(*f.root, "figures"))
	}
	return nil
}

func decode(path string) (image.Image, error) {
	r, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	img, _, err := image.Decode(r)
	return img, err
}

type subImager interface {
	SubImage(image.Rectangle) image.Image
}

func crop(img image.Image, box image.Rectangle, path string, quality int) error {
	s, ok := img.(subImager)
	if !ok {
		return fmt.Errorf("%T cannot be cropped", img)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	w, err := os.Create(path)
	if err != nil {
		return err
	}
	b := img.Bounds()
	if err := jpeg.Encode(w, s.SubImage(box.Add(b.Min)), &jpeg.Options{Quality: quality}); err != nil {
		w.Close()
		return err
	}
	return w.Close()
}
