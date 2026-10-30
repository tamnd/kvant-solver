// The one script of the site.
//
// Every page reads without it: the articles, the mathematics, the contents of
// an issue and every link were settled when the site was built. What it adds
// is search, the theme button, the menus on a phone, the keys, and the jump
// from a tag's row to the thing the tag names.
(function () {
  "use strict";
  var root = document.documentElement;
  var body = document.body;

  // Theme. assets/theme.js sets the saved choice before the first paint; this
  // is the button.
  function theme() {
    return root.dataset.theme || (matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light");
  }
  document.querySelectorAll("[data-theme-toggle]").forEach(function (b) {
    b.addEventListener("click", function () {
      var t = theme() === "dark" ? "light" : "dark";
      root.dataset.theme = t;
      try { localStorage.setItem("theme", t); } catch (e) {}
    });
  });

  // The menu and the contents on a narrow screen.
  function toggler(attr, cls) {
    document.querySelectorAll("[data-toggle=" + attr + "]").forEach(function (b) {
      b.addEventListener("click", function (e) {
        e.stopPropagation();
        var open = body.classList.toggle(cls);
        b.setAttribute("aria-expanded", open ? "true" : "false");
      });
    });
  }
  toggler("nav", "nav-open");
  toggler("side", "side-open");
  document.addEventListener("click", function (e) {
    if (!e.target.closest(".side, .primary")) body.classList.remove("nav-open", "side-open");
  });

  // Keys: / to search, [ and ] for the previous and the next page, Escape to
  // close whatever is open.
  document.addEventListener("keydown", function (e) {
    if (e.metaKey || e.ctrlKey || e.altKey) return;
    var t = e.target;
    var typing = t && (t.tagName === "INPUT" || t.tagName === "TEXTAREA" || t.isContentEditable);
    if (e.key === "Escape") {
      body.classList.remove("nav-open", "side-open");
      if (typing) t.blur();
      return;
    }
    if (typing) return;
    if (e.key === "/") {
      var f = document.querySelector(".find-big input, .find input");
      if (f) { e.preventDefault(); f.focus(); f.select(); }
    } else if (e.key === "[" || e.key === "]") {
      var a = document.querySelector(e.key === "[" ? "a[rel=prev]" : "a[rel=next]");
      if (a) location.href = a.href;
    }
  });

  // The entry for this page in the contents beside it, scrolled into view.
  var side = document.querySelector(".side");
  var current = side && side.querySelector("[aria-current]");
  if (current) {
    var r = current.getBoundingClientRect(), s = side.getBoundingClientRect();
    if (r.top < s.top || r.bottom > s.bottom) side.scrollTop += r.top - s.top - s.height / 3;
  }

  // On the sheets of an issue, the sheet being read is marked in the contents.
  var sheets = document.querySelectorAll(".sheet[id]");
  if (side && sheets.length && "IntersectionObserver" in window) {
    var links = {};
    side.querySelectorAll("a[href*='#']").forEach(function (a) { links[a.hash.slice(1)] = a; });
    var on = null;
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (en) {
        if (!en.isIntersecting) return;
        var a = links[en.target.id];
        if (!a || a === on) return;
        if (on) on.classList.remove("on");
        a.classList.add("on");
        on = a;
        var r = a.getBoundingClientRect(), s = side.getBoundingClientRect();
        if (r.top < s.top || r.bottom > s.bottom) side.scrollTop += r.top - s.top - s.height / 2;
      });
    }, { rootMargin: "-20% 0px -70% 0px" });
    sheets.forEach(function (el) { io.observe(el); });
  }

  // A tag's address is its row on the page of tags that share its first
  // character, and the row knows where the thing it names is now. Arriving at
  // the row goes on to the thing, which is what following a permanent link
  // should do.
  function followTag() {
    var id = decodeURIComponent(location.hash.slice(1));
    if (!id) return;
    var row = document.getElementById(id);
    if (row && row.dataset.go) location.replace(row.dataset.go);
  }
  if (document.querySelector("[data-tags]")) {
    followTag();
    window.addEventListener("hashchange", followTag);
  }
  document.querySelectorAll("form.lookup").forEach(function (f) {
    f.addEventListener("submit", function (e) {
      e.preventDefault();
      var tag = (f.querySelector("input").value || "").trim().toUpperCase();
      if (/^[0-9A-Z]{4}$/.test(tag)) location.href = f.dataset.base + tag.charAt(0) + "#" + tag;
    });
  });

  // Search. The index is one file per language, built with the site.
  var results = document.querySelector("[data-search]");
  if (!results) return;
  var input = document.querySelector(".find-big input");
  var status = document.querySelector(".search-status");
  var base = results.dataset.root || "";
  var words = { none: results.dataset.none, empty: results.dataset.empty };
  var records = null;

  function fold(s) {
    return (s || "").toLowerCase().replace(/ё/g, "е").normalize("NFD").replace(/[\u0300-\u036f]/g, "").replace(/đ/g, "d");
  }
  function escapeHTML(s) {
    return String(s).replace(/[&<>"]/g, function (c) { return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" }[c]; });
  }
  function score(rec, terms, raw) {
    var total = 0;
    for (var i = 0; i < terms.length; i++) {
      var t = terms[i], s = 0;
      var at = rec._t.indexOf(t);
      if (at === 0) s = 6; else if (at > 0) s = 4;
      if (rec._a.indexOf(t) >= 0) s = Math.max(s, 3);
      if (rec._r.indexOf(t) >= 0) s = Math.max(s, 2);
      if (rec._i.indexOf(t) >= 0) s = Math.max(s, 1);
      if (!s) return 0;
      total += s;
    }
    if (rec.g && rec.g === raw.toUpperCase()) total += 50;
    return total;
  }
  function render(q) {
    var raw = q.trim();
    if (!raw) { results.innerHTML = ""; status.textContent = words.empty; return; }
    var terms = fold(raw).split(/\s+/).filter(Boolean);
    var hits = [];
    for (var i = 0; i < records.length; i++) {
      var s = score(records[i], terms, raw);
      if (s) hits.push([s, i]);
    }
    hits.sort(function (a, b) { return b[0] - a[0] || a[1] - b[1]; });
    status.textContent = hits.length ? hits.length + "" : words.none;
    var html = "";
    hits.slice(0, 200).forEach(function (h) {
      var r = records[h[1]];
      var sub = [];
      if (r.r) sub.push('<span class="r">' + escapeHTML(r.r) + "</span>");
      if (r.a) sub.push(escapeHTML(r.a));
      html += '<li><a href="' + escapeHTML(base + r.h) + '"><span class="t">' + escapeHTML(r.t) + '</span><span class="n">' +
        escapeHTML(r.i || "") + (r.g ? " · " + escapeHTML(r.g) : "") + "</span>" +
        (sub.length ? '<span class="s">' + sub.join(" ") + "</span>" : "") + "</a></li>";
    });
    results.innerHTML = html;
  }
  function run() {
    var q = input.value;
    var u = new URL(location.href);
    if (q) u.searchParams.set("q", q); else u.searchParams.delete("q");
    history.replaceState(null, "", u);
    render(q);
  }
  var params = new URLSearchParams(location.search);
  input.value = params.get("q") || "";
  document.querySelectorAll(".find input").forEach(function (i) { i.value = input.value; });
  fetch(results.dataset.search, { credentials: "same-origin" })
    .then(function (r) { return r.ok ? r.json() : Promise.reject(r.status); })
    .then(function (data) {
      records = data.records;
      records.forEach(function (r) { r._t = fold(r.t); r._a = fold(r.a); r._r = fold(r.r); r._i = fold(r.i + " " + (r.g || "")); });
      var timer = 0;
      input.addEventListener("input", function () { clearTimeout(timer); timer = setTimeout(run, 80); });
      input.form.addEventListener("submit", function (e) { e.preventDefault(); run(); });
      render(input.value);
      input.focus();
    })
    .catch(function () { status.textContent = words.none; });
})();
