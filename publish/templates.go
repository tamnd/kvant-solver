package publish

import (
	_ "embed"
	"html/template"
)

// The frame every page is written into.
//
// One layout, one stylesheet and one script, the last two embedded from
// assets/, because the whole point of this package is that nothing about the
// build needs a second toolchain.
//
// The script is an addition and never a requirement. A reader with scripting
// off sees the finished article, the typeset mathematics, the contents of the
// issue and every link, because all of that was decided here rather than in
// the browser. What they lose is search, the theme button and the keys.

//go:embed assets/site.css
var siteCSS string

//go:embed assets/site.js
var siteJS string

//go:embed assets/theme.js
var themeJS string

// icon is the mark in the browser tab: the magazine's initial on its red.
// It is written into every page rather than shipped as a file, since it is a
// hundred and fifty bytes and a file would be one more request on every
// first visit.
const icon = `data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 32 32'%3E%3Crect width='32' height='32' fill='%23c0281f'/%3E%3Ctext x='16' y='24' font-family='Georgia,serif' font-size='22' font-weight='700' fill='white' text-anchor='middle'%3E%D0%9A%3C/text%3E%3C/svg%3E`

const layout = `<!doctype html>
<html lang="{{.Lang}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{.Title}}</title>
{{with .Description}}<meta name="description" content="{{.}}">
{{end}}<meta name="color-scheme" content="light dark">
<meta property="og:title" content="{{.Title}}">
<meta property="og:site_name" content="{{.T.Brand}}">
{{with .Canonical}}<link rel="canonical" href="{{.}}">
{{end}}{{range .Alternates}}<link rel="alternate" hreflang="{{.Lang}}" href="{{.Href}}">
{{end}}<link rel="icon" href="{{.Icon}}">
<script src="{{.Root}}assets/theme.js"></script>
<link rel="stylesheet" href="{{.Root}}assets/katex.min.css">
<link rel="stylesheet" href="{{.Root}}assets/site.css">
<script defer src="{{.Root}}assets/site.js"></script>
</head>
<body>
<a class="skip" href="#main">{{.T.Skip}}</a>
<header class="topbar">
{{if .Nav}}<button class="icon-btn menu" type="button" data-toggle="nav" aria-label="{{.T.Menu}}" aria-expanded="false"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M4 7h16M4 12h16M4 17h16"/></svg></button>
{{end}}{{if .Side}}<button class="icon-btn side-btn" type="button" data-toggle="side" aria-label="{{.T.Contents}}" aria-expanded="false"><svg viewBox="0 0 24 24" aria-hidden="true"><path d="M9 6h11M9 12h11M9 18h11M4 6h1M4 12h1M4 18h1"/></svg></button>
{{end}}<a class="brand" href="{{.HomeHref}}"><span class="logo">К</span><span class="brand-text">{{.T.Brand}}</span></a>
{{if .Nav}}<nav class="primary" aria-label="{{.T.Archive}}">{{range .Nav}}<a href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}>{{.Text}}</a>{{end}}</nav>
<form class="find" action="{{.SearchHref}}" method="get" role="search"><input name="q" type="search" placeholder="{{.T.SearchHint}}" aria-label="{{.T.Search}}" autocomplete="off"><kbd>/</kbd></form>
<a class="icon-btn search-btn" href="{{.SearchHref}}" aria-label="{{.T.Search}}"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="11" cy="11" r="6"/><path d="M20 20l-4.5-4.5"/></svg></a>
{{end}}{{if .Langs}}<nav class="langs" aria-label="{{.T.Languages}}">{{range .Langs}}{{if .Current}}<span class="here" title="{{.Name}}">{{.Lang}}</span>{{else}}<a href="{{.Href}}" hreflang="{{.Lang}}" lang="{{.Lang}}" title="{{.Name}}">{{.Lang}}</a>{{end}}{{end}}</nav>
{{end}}<button class="icon-btn" type="button" data-theme-toggle aria-label="{{.T.Theme}}"><svg viewBox="0 0 24 24" aria-hidden="true"><circle cx="12" cy="12" r="8"/><path d="M12 4a8 8 0 0 1 0 16z" fill="currentColor"/></svg></button>
</header>
<div class="shell{{if .Side}} has-side{{end}}">
{{with .Side}}<aside class="side" id="side" aria-label="{{$.T.Contents}}">
<a class="side-head" href="{{.Href}}"><span class="eyebrow">{{.Kicker}}</span><span class="title">{{.Title}}</span></a>
<ol>
{{range .Items}}<li><a href="{{.Href}}"{{if .Current}} aria-current="page"{{end}}><span>{{.Text}}</span>{{if .Note}}<span class="n">{{.Note}}</span>{{end}}</a></li>
{{end}}</ol>
{{with .More}}<a class="more" href="{{.Href}}">{{.Text}}</a>
{{end}}</aside>
{{end}}<main id="main">
{{if .Crumbs}}<nav class="crumbs" aria-label="Breadcrumb">{{range $i, $c := .Crumbs}}{{if $i}}<span class="sep">/</span>{{end}}<a href="{{$c.Href}}">{{$c.Text}}</a>{{end}}</nav>
{{end}}{{template "body" .}}
{{if or .Prev .Next}}<nav class="pager" aria-label="{{.T.Prev}} / {{.T.Next}}">{{with .Prev}}<a class="prev" href="{{.Href}}" rel="prev"><span class="l">&larr; {{.Note}}</span><span class="t">{{.Text}}</span></a>{{end}}{{with .Next}}<a class="next" href="{{.Href}}" rel="next"><span class="l">{{.Note}} &rarr;</span><span class="t">{{.Text}}</span></a>{{end}}</nav>
{{end}}{{with .Foot}}<p class="foot-note">{{.}}</p>
{{end}}</main>
</div>
<footer class="site-foot"><div class="in">
<span>{{.T.Brand}} · {{.T.Tagline}}</span>
<a href="https://github.com/tamnd/kvant">{{.T.Source}}</a>
{{if .FootLangs}}<span class="langs-foot">{{range .FootLangs}}<a href="{{.Href}}" hreflang="{{.Lang}}" lang="{{.Lang}}">{{.Name}}</a>{{end}}</span>
{{end}}</div></footer>
</body>
</html>
`

func mustParse(name, body string) *template.Template {
	t := template.Must(template.New(name).Parse(layout))
	template.Must(t.New("body").Parse(body))
	return t
}

// rows is the one list of the site: a title, a note to the right of it and a
// line under it. The archive, the contents of an issue, the sections, the
// authors and the problems are all this list with different things in it, and
// writing it once is what keeps them looking like one site.
const rows = `{{define "rows"}}<ul class="rows">
{{range .}}<li{{with .ID}} id="{{.}}"{{end}}><a href="{{.Href}}"><span class="t">{{.Text}}</span>{{if .Note}}<span class="n">{{.Note}}</span>{{end}}{{if or .Kicker .Sub}}<span class="s">{{with .Kicker}}<span class="r">{{.}}</span>{{end}}{{.Sub}}</span>{{end}}</a></li>
{{end}}</ul>{{end}}`

// landingTmpl is the root of the site, which belongs to no language and so
// says what the magazine is in every one it is published in.
var landingTmpl = mustParse("landing", `
<section class="hero">
<h1>Квант · Kvant</h1>
{{range .Data.Langs}}<p class="lede" lang="{{.Lang}}">{{.Tagline}}</p>
{{end}}</section>
<ul class="langcards">
{{range .Data.Langs}}<li><a href="{{.Href}}" lang="{{.Lang}}" hreflang="{{.Lang}}"><span class="k">{{.Name}}</span><span class="t">{{.Brand}}</span><span class="d">{{.About}}</span><span class="n">{{.Counts}}</span></a></li>
{{end}}</ul>
`)

// homeTmpl is the archive in one language: every year, and every issue of it
// that has been read as a tile, so that any issue is two clicks from anywhere.
var homeTmpl = mustParse("home", rows+`
<section class="hero">
<span class="eyebrow">{{.T.Tagline}}</span>
<h1>{{.T.Brand}}</h1>
<p class="lede">{{.T.About}}</p>
<form class="find-big" action="{{.SearchHref}}" method="get" role="search"><input name="q" type="search" placeholder="{{.T.SearchHint}}" aria-label="{{.T.Search}}" autocomplete="off"><button type="submit">{{.T.Search}}</button></form>
<div class="stats">{{range .Data.Stats}}<div><b>{{.Text}}</b><span>{{.Note}}</span></div>{{end}}</div>
</section>
<ul class="quick">{{range .Data.Quick}}<li><a href="{{.Href}}">{{.Text}}<span>{{.Note}}</span></a></li>{{end}}</ul>
{{range .Data.Decades}}<section class="decade">
<h2>{{.Title}}</h2>
<div class="years">
{{range .Years}}<div class="year"><a href="{{.Href}}">{{.Year}}</a><ul class="tiles">{{range .Issues}}<li><a href="{{.Href}}" title="{{.Note}}">{{.Text}}</a></li>{{end}}</ul></div>
{{end}}</div>
</section>
{{else}}<p class="empty">{{.T.Empty}}</p>
{{end}}`)

// yearTmpl is one year, its issues as cards with the opening of their
// contents on them.
var yearTmpl = mustParse("year", `
<div class="head"><h1>{{.Title}}</h1>{{with .Data.Lede}}<p class="lede">{{.}}</p>{{end}}</div>
<ul class="cards">
{{range .Data.Issues}}<li><a href="{{.Href}}"><span class="k">{{$.T.Issue}}</span><span class="t">№ {{.Text}}</span><span class="n">{{.Note}}</span>{{if .Titles}}<ul>{{range .Titles}}<li>{{.}}</li>{{end}}</ul>{{end}}</a></li>
{{end}}</ul>
`)

// issueTmpl is one issue: its articles and the way to its sheets.
//
// Both are there because both are real. The articles are what the magazine
// published and the sheets are what was read, and an article is a view of
// sheets rather than a replacement for them: anything no article claimed
// survives only on the sheet it was printed on.
var issueTmpl = mustParse("issue", rows+`
<div class="head">
<span class="eyebrow"><a href="{{.Data.YearHref}}">{{.Data.Year}}</a></span>
<h1>{{.Title}}</h1>
<div class="meta">{{range .Data.Chips}}{{if .Href}}<a class="chip" href="{{.Href}}">{{.Text}}</a>{{else}}<span class="chip">{{.Text}}</span>{{end}}{{end}}</div>
</div>
<h2>{{.T.Articles}}</h2>
{{if .Data.Articles}}{{template "rows" .Data.Articles}}{{else}}<p class="empty">{{.T.NoArticles}}</p>{{end}}
`)

// articleTmpl is one article. Everything in the head that names something the
// site also indexes is a link into that index.
var articleTmpl = mustParse("article", `
<article>
<div class="head">
{{with .Data.Rubric}}<span class="eyebrow"><a href="{{.Href}}">{{.Text}}</a></span>{{end}}
<h1>{{.Title}}</h1>
{{if .Data.Authors}}<p class="by">{{range $i, $a := .Data.Authors}}{{if $i}}, {{end}}<a href="{{$a.Href}}">{{$a.Text}}</a>{{end}}</p>{{end}}
<div class="meta">{{range .Data.Chips}}{{if .Href}}<a class="chip{{with .Kicker}} {{.}}{{end}}" href="{{.Href}}">{{.Text}}</a>{{else}}<span class="chip">{{.Text}}</span>{{end}}{{end}}</div>
</div>
{{with .Data.Notice}}<p class="notice">{{.Text}}{{with .Href}} <a href="{{.}}">{{$.T.Original}} &rarr;</a>{{end}}</p>{{end}}
<div class="doc">
{{.Data.Body}}
</div>
</article>
`)

// sheetsTmpl is every sheet of one issue as it was read, one under the other.
//
// They are one page rather than one page each because an issue is read in
// order, because the sheets of a magazine are short, and because a page each
// was two thirds of the files of the site. The provenance is on every sheet,
// because a sheet lifted out of a born digital PDF and a sheet a model read off
// a scan are not the same kind of evidence.
var sheetsTmpl = mustParse("sheets", `
<div class="head">
<span class="eyebrow"><a href="{{.Data.IssueHref}}">{{.Data.Issue}}</a></span>
<h1>{{.Title}}</h1>
</div>
{{range .Data.Sheets}}<section class="sheet" id="{{.ID}}">
<div class="sheet-head"><h2><a href="#{{.ID}}">{{.Text}}</a></h2>{{with .Label}}<span>{{.}}</span>{{end}}{{with .How}}<span class="how">{{.}}</span>{{end}}</div>
<div class="doc">
{{.Body}}
</div>
</section>
{{else}}<p class="empty">{{.T.NoPages}}</p>
{{end}}`)

// listTmpl is every view that is a list with a line of explanation over it.
var listTmpl = mustParse("list", rows+`
<div class="head">{{with .Data.Kicker}}<span class="eyebrow">{{.}}</span>{{end}}<h1>{{.Title}}</h1>{{with .Data.Lede}}<p class="lede">{{.}}</p>{{end}}</div>
{{if .Data.Letters}}<nav class="letters">{{range .Data.Groups}}<a href="#{{.ID}}">{{.Title}}</a>{{end}}</nav>{{end}}
{{if .Data.Groups}}{{range .Data.Groups}}<section class="group" id="{{.ID}}">{{if .Title}}<h2>{{.Title}}</h2>{{end}}
{{template "rows" .Items}}
</section>
{{end}}{{else}}{{if .Data.Items}}{{template "rows" .Data.Items}}{{else}}<p class="empty">{{.T.Empty}}</p>{{end}}{{end}}
`)

// problemTmpl is one problem of the Задачник, with both of its printed halves.
//
// The two issues are linked only when the site has them. A problem posed in an
// issue nobody has read yet is still worth publishing, because the number is
// what the olympiad literature cites and a reader who follows a citation should
// land on the statement rather than on nothing.
var problemTmpl = mustParse("problem", `
<article class="problem">
<div class="head">
<span class="eyebrow"><a href="{{.Data.IndexHref}}">{{.T.Problems}}</a></span>
<h1>{{.Title}}</h1>
{{with .Data.Authors}}<p class="by">{{.}}</p>{{end}}
<div class="meta">{{range .Data.Chips}}{{if .Href}}<a class="chip{{with .Kicker}} {{.}}{{end}}" href="{{.Href}}">{{.Text}}</a>{{else}}<span class="chip">{{.Text}}</span>{{end}}{{end}}</div>
</div>
<div class="doc">
{{.Data.Body}}
</div>
</article>
`)

// tagsTmpl is one shard of the tags: every tag that starts with one character,
// each row the permanent address of what it names.
var tagsTmpl = mustParse("tags", `
<div class="head"><span class="eyebrow"><a href="{{.Data.IndexHref}}">{{.T.Tags}}</a></span><h1>{{.Title}}</h1><p class="lede">{{.T.TagLede}}</p></div>
<ul class="rows" data-tags>
{{range .Data.Tags}}<li id="{{.ID}}" data-go="{{.Go}}"><div class="row"><a class="t" href="{{.Go}}"><span class="tag">{{.ID}}</span> {{.Text}}</a><span class="n">{{.Note}}</span>{{if .Alts}}<span class="alts">{{range .Alts}}<a href="{{.Href}}" hreflang="{{.Lang}}">{{.Lang}}</a>{{end}}</span>{{end}}</div></li>
{{end}}</ul>
`)

// tagIndexTmpl is the way to a tag by hand: a box to type it in and the
// thirty six pages they are filed on.
var tagIndexTmpl = mustParse("tagindex", rows+`
<div class="head"><h1>{{.Title}}</h1><p class="lede">{{.T.TagIndexLede}}</p></div>
<form class="lookup" data-base="{{.Data.Base}}"><input maxlength="4" placeholder="{{.T.TagLookup}}" aria-label="{{.T.TagLookup}}" autocomplete="off" spellcheck="false"><button type="submit">&rarr;</button></form>
{{template "rows" .Data.Items}}
`)

// searchTmpl is the search page. The box works without the script only in the
// sense that it submits; the matching is done in the browser against the index
// the build wrote next to it.
var searchTmpl = mustParse("search", rows+`
<div class="search-page">
<div class="head"><h1>{{.T.Search}}</h1></div>
<form class="find-big" action="" method="get" role="search"><input name="q" type="search" placeholder="{{.T.SearchHint}}" aria-label="{{.T.Search}}" autocomplete="off"></form>
<p class="search-status" aria-live="polite"><noscript>{{.T.SearchNoJS}}</noscript></p>
<ul class="rows" data-search="{{.Data.Index}}" data-root="{{.Root}}" data-none="{{.T.SearchNone}}" data-empty="{{.T.SearchEmpty}}"></ul>
</div>
`)

// notFoundTmpl is what both hosts serve for an address that holds nothing.
var notFoundTmpl = mustParse("notfound", rows+`
<div class="head"><h1>{{.Title}}</h1><p class="lede">{{.T.NotFoundLede}}</p></div>
{{template "rows" .Data.Items}}
`)
