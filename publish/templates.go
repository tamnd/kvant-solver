package publish

import "html/template"

// The templates are here rather than in files because they are small, because
// embedding them would put the site's markup out of reach of the guard's own
// tests,
// and because the whole point of this package is that nothing about the build
// needs a second toolchain.
//
// There is no JavaScript in any of them. A reader with scripting off sees the
// finished article, the typeset mathematics and every link, because all of that
// was decided here rather than in the browser. That is a requirement of the
// milestone and it is also the only honest way to publish a corpus whose
// content is mathematics.

// The four views in the header are written on every build even when they are
// empty, so that the link in this layout always lands somewhere. A corpus with
// no problems in it yet still gets a problems page that says so, which is a
// smaller thing to maintain than a header that changes shape depending on what
// has been read.
const layout = `<!doctype html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
<link rel="stylesheet" href="{{.Root}}assets/katex.min.css">
<link rel="stylesheet" href="{{.Root}}assets/site.css">
</head>
<body>
<header>
<a class="home" href="{{.Root}}index.html">Квант</a>
<nav>
<a href="{{.Root}}rubrics/index.html">рубрики</a>
<a href="{{.Root}}authors/index.html">авторы</a>
<a href="{{.Root}}problems/index.html">задачи</a>
<a href="{{.Root}}tags/index.html">теги</a>
</nav>
</header>
<main>
{{template "body" .}}
</main>
</body>
</html>
`

func mustParse(name, body string) *template.Template {
	t := template.Must(template.New(name).Parse(layout))
	return template.Must(t.New("body").Parse(body))
}

// listTmpl is the archive, each year, and every view that is a list of things
// with a line of explanation over it. They are the same page with different
// things on it, and writing four more templates to say so would be four more
// places for the markup to drift.
var listTmpl = mustParse("list", `
<h1>{{.Title}}</h1>
{{if .Lede}}<p class="lede">{{.Lede}}</p>{{end}}
<ul class="list">
{{range .Items}}<li><a href="{{.Href}}">{{.Title}}</a>{{if .Note}} <span class="note">{{.Note}}</span>{{end}}</li>
{{else}}<li class="note">пока пусто</li>
{{end}}</ul>
`)

// issueTmpl is one issue, articles first and then the sheets.
//
// Both are listed because both are real. The articles are what the magazine
// published and the pages are what was read, and an article is a view of pages
// rather than a replacement for them: anything no article claimed survives only
// on the sheet it was printed on.
var issueTmpl = mustParse("issue", `
<h1>{{.Title}}</h1>
<h2>Статьи</h2>
<ul class="list">
{{range .Articles}}<li><a href="{{.Href}}">{{.Title}}</a>{{if .Note}} <span class="note">{{.Note}}</span>{{end}}</li>
{{else}}<li class="note">номер ещё не разобран на статьи</li>
{{end}}</ul>
<h2>Листы</h2>
<ul class="sheets">
{{range .Pages}}<li><a href="{{.Href}}">{{.Title}}</a>{{if .Note}} <span class="note">{{.Note}}</span>{{end}}</li>
{{else}}<li class="note">ни один лист ещё не прочитан</li>
{{end}}</ul>
`)

// articleTmpl is one article. Everything in the meta line that names something
// the site also indexes is a link to that index, because a rubric page nothing
// points at is a page nobody will find.
var articleTmpl = mustParse("article", `
<article>
<h1>{{.Title}}</h1>
<p class="meta">
{{if .Authors}}<span class="authors">{{range $i, $a := .Authors}}{{if $i}}, {{end}}<a href="{{$a.Href}}">{{$a.Title}}</a>{{end}}</span>{{end}}
{{with .Rubric}}<span class="rubric"><a href="{{.Href}}">{{.Title}}</a></span>{{end}}
<span class="issue"><a href="{{.Up}}index.html">{{.Issue}}</a></span>
{{if .Pages}}<span class="pages">с. {{.Pages}}</span>{{end}}
{{with .Tag}}<span class="tag"><a href="{{.Href}}">{{.Title}}</a></span>{{end}}
</p>
{{.Body}}
</article>
`)

// problemTmpl is one problem of the Задачник, with both of its printed halves.
//
// The two issues are linked only when the site has them. A problem posed in an
// issue nobody has read yet is still worth publishing, because the number is
// what the olympiad literature cites and a reader who follows a citation should
// land on the statement rather than on nothing.
var problemTmpl = mustParse("problem", `
<article class="problem">
<h1>{{.Title}}</h1>
<p class="meta">
{{if .Authors}}<span class="authors">{{.Authors}}</span>{{end}}
{{with .Posed}}<span class="posed">условие: {{if .Href}}<a href="{{.Href}}">{{.Title}}</a>{{else}}{{.Title}}{{end}}{{if .Note}}, с. {{.Note}}{{end}}</span>{{end}}
{{with .Solved}}<span class="solved">решение: {{if .Href}}<a href="{{.Href}}">{{.Title}}</a>{{else}}{{.Title}}{{end}}{{if .Note}}, с. {{.Note}}{{end}}</span>{{end}}
{{with .Tag}}<span class="tag"><a href="{{.Href}}">{{.Title}}</a></span>{{end}}
</p>
{{.Body}}
</article>
`)

// tagTmpl is one tag.
//
// A tag is a permanent address and the path of the thing it names is not: an
// article's file carries a slug made out of its title, and a retitled article
// moves. So the tag gets a page of its own that does nothing but point at
// wherever the object is now, and a citation written against the tag keeps
// working when the object moves. This is the Stacks Project's arrangement and
// it is the reason the tags file exists at all.
var tagTmpl = mustParse("tag", `
<h1>Тег {{.Tag}}</h1>
<p class="lede">Постоянный адрес. Он закреплён за материалом навсегда, даже если тот сменит название или путь.</p>
<ul class="list">
<li><a href="{{.Href}}">{{.Title}}</a>{{if .Note}} <span class="note">{{.Note}}</span>{{end}}</li>
</ul>
`)

// pageTmpl is one sheet as it was read.
//
// The provenance is on the page and not hidden in the repository, because a
// sheet lifted out of a born digital PDF and a sheet a model read off a scan
// are not the same kind of evidence, and a reader comparing two readings of the
// same paper should be able to see which is which.
var pageTmpl = mustParse("page", `
<article class="sheet">
<h1>{{.Title}}</h1>
<p class="meta">
{{if .Label}}<span class="folio">напечатано: {{.Label}}</span>{{end}}
<span class="issue"><a href="{{.Up}}index.html">номер</a></span>
{{if .Extraction}}<span class="how">{{.Extraction}}{{if .Model}}: {{.Model}}{{end}}</span>{{end}}
</p>
{{.Body}}
</article>
`)

// siteCSS is the whole of the site's own styling. KaTeX brings its own.
const siteCSS = `:root { --ink: #1a1a1a; --faint: #6b6b6b; --rule: #e0ddd6; --paper: #fbfaf7; }
* { box-sizing: border-box; }
body { margin: 0; background: var(--paper); color: var(--ink);
  font: 17px/1.65 Georgia, "Times New Roman", serif; }
header { border-bottom: 1px solid var(--rule); padding: 1rem 1.5rem;
  display: flex; flex-wrap: wrap; gap: .5rem 1.5rem; align-items: baseline; }
header a { color: var(--ink); font-weight: bold; letter-spacing: .06em;
  text-transform: uppercase; text-decoration: none; font-size: .85rem; }
header nav { display: flex; flex-wrap: wrap; gap: .5rem 1rem; }
header nav a { color: var(--faint); font-weight: normal; }
main { max-width: 40rem; margin: 0 auto; padding: 2rem 1.5rem 6rem; }
h1 { font-size: 1.6rem; line-height: 1.25; margin: 0 0 1rem; }
h2 { font-size: 1.1rem; margin: 2.5rem 0 .75rem; }
a { color: #23527c; }
p { margin: 0 0 1.1rem; }
.meta { color: var(--faint); font-size: .85rem; margin-bottom: 2rem;
  border-bottom: 1px solid var(--rule); padding-bottom: 1rem; }
.meta span + span::before { content: " · "; }
.note { color: var(--faint); font-size: .85rem; }
.lede { color: var(--faint); font-size: .9rem; margin-top: -.5rem; }
ul.list, ul.sheets { list-style: none; padding: 0; }
ul.list li { margin-bottom: .5rem; }
ul.sheets { display: flex; flex-wrap: wrap; gap: .5rem 1rem; }
ul.sheets li { font-size: .9rem; }

/* The marks are the reading's record of the shape of the sheet. A figure is
   the one that has to be visible: the corpus holds the magazine's text and none
   of its pictures, and an article that says look at the figure is unreadable if
   the page is silent about where the figure was. */
.mark { color: var(--faint); font-size: .8rem; }
/* The mark carries a caption when the reading found one, so the label needs a
   space after it and nothing after that when there is no caption. */
.mark.figure::before { content: "[рисунок]\a0"; }
.mark.column { display: block; height: .6rem; }
.mark.rubric { display: block; height: .6rem; }
.mark.folio { float: right; margin-left: 1rem; font-size: .75rem;
  color: var(--faint); }
.mark.folio::before { content: "с. "; }

/* A formula KaTeX would not take. The source is shown rather than hidden,
   because the reading failed here and pretending otherwise helps nobody. */
.tex-failed { background: #fdf0ee; border-bottom: 1px dotted #b04a3a;
  font-size: .9em; }

.katex { font-size: 1em; }
.katex-display { overflow-x: auto; overflow-y: hidden; padding: .25rem 0; }
@media (max-width: 30rem) { main { padding: 1.5rem 1rem 4rem; } }
`
