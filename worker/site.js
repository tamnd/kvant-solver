// The Worker in front of kvant.tamnd.com. It serves the site that kvant deploy
// packed into R2: the manifest names the index shards, a shard says which pack
// an address is in and where, and the page is one ranged read of that pack.
// See sitepack/sitepack.go for the format; Shard and the address rules here
// must match it.

const SHARDS = 256;
const MANIFEST_TTL = 30_000;

let manifest = null;
let manifestAt = 0;
const shards = new Map();

function shardOf(key) {
  let h = 2166136261;
  for (const b of new TextEncoder().encode(key)) {
    h ^= b;
    h = Math.imul(h, 16777619) >>> 0;
  }
  return h % SHARDS;
}

async function getManifest(env) {
  if (manifest && Date.now() - manifestAt < MANIFEST_TTL) return manifest;
  const obj = await env.SITE.get("site.json");
  if (!obj) throw new Error("no site.json");
  manifest = await obj.json();
  // The deploy writes the manifest last, so its upload time is when every page
  // of this build became what it is, which is the Last-Modified they share.
  manifest.modified = new Date(Math.floor(obj.uploaded.getTime() / 1000) * 1000);
  manifestAt = Date.now();
  return manifest;
}

async function lookup(env, m, key) {
  const name = m.shards[shardOf(key)];
  let shard = shards.get(name);
  if (!shard) {
    const obj = await env.SITE.get(name);
    if (!obj) return null;
    shard = await obj.json();
    if (shards.size > 512) shards.clear();
    shards.set(name, shard);
  }
  return shard[key] || null;
}

function cacheControl(key, type) {
  if (key.startsWith("assets/")) return "public, max-age=86400, stale-while-revalidate=604800";
  if (key.endsWith("search.json")) return "public, max-age=3600";
  if (type.startsWith("text/html")) return "public, max-age=300, stale-while-revalidate=86400";
  return "public, max-age=3600";
}

// matches reads an If-None-Match list, weak or strong, the way RFC 9110 says a
// GET compares validators.
function matches(header, etag) {
  if (!header) return false;
  return header.split(",").some((t) => {
    t = t.trim();
    return t === "*" || t.replace(/^W\//, "") === `"${etag}"`;
  });
}

// fresh says whether the reader's copy is this one. The edge strips ETag from
// the HTML it may rewrite, weak or not, so the build time stands in for it
// there; an ETag that does reach the reader still wins, as RFC 9110 orders it.
function fresh(request, etag, modified) {
  const inm = request.headers.get("If-None-Match");
  if (inm) return matches(inm, etag);
  const ims = Date.parse(request.headers.get("If-Modified-Since") || "");
  return !Number.isNaN(ims) && ims >= modified.getTime();
}

async function serve(request, env, ctx, m, key, entry, status) {
  const [pack, offset, length, type, etag, gz] = entry;
  // Weak, because the edge drops a strong validator from any text response it
  // might compress, and a page that never answers 304 is sent whole every time.
  const tag = `W/"${etag}"`;
  const headers = new Headers({
    "Content-Type": type,
    "ETag": tag,
    "Cache-Control": status === 200 ? cacheControl(key, type) : "no-store",
    "X-Content-Type-Options": "nosniff",
    "Referrer-Policy": "strict-origin-when-cross-origin",
    "Vary": "Accept-Encoding",
  });
  if (status === 200) headers.set("Last-Modified", m.modified.toUTCString());
  if (status === 200 && fresh(request, etag, m.modified)) {
    return new Response(null, { status: 304, headers });
  }
  if (request.method === "HEAD") return new Response(null, { status, headers });
  // The edge cache holds the stored bytes under their hash, so a page is read
  // out of R2 once per location however many readers ask for it.
  const cache = caches.default;
  const cacheKey = new Request(`https://kvant-pack.internal/${etag}/${gz}`);
  let body = null;
  const hit = await cache.match(cacheKey);
  if (hit) {
    body = await hit.arrayBuffer();
  } else {
    const obj = await env.SITE.get(pack, { range: { offset, length } });
    if (!obj) return new Response("pack missing", { status: 503 });
    body = await obj.arrayBuffer();
    ctx.waitUntil(cache.put(cacheKey, new Response(body, { headers: { "Cache-Control": "public, max-age=604800" } })));
  }
  if (gz) {
    if ((request.headers.get("Accept-Encoding") || "").includes("gzip")) {
      headers.set("Content-Encoding", "gzip");
      return new Response(body, { status, headers, encodeBody: "manual" });
    }
    const plain = new Response(body).body.pipeThrough(new DecompressionStream("gzip"));
    return new Response(plain, { status, headers });
  }
  return new Response(body, { status, headers });
}

export default {
  async fetch(request, env, ctx) {
    if (request.method !== "GET" && request.method !== "HEAD") {
      return new Response("method not allowed", { status: 405, headers: { Allow: "GET, HEAD" } });
    }
    const url = new URL(request.url);
    let key;
    try {
      key = decodeURIComponent(url.pathname).replace(/^\/+/, "");
    } catch {
      key = url.pathname.replace(/^\/+/, "");
    }
    const m = await getManifest(env);

    let entry = await lookup(env, m, key);
    if (entry) return serve(request, env, ctx, m, key, entry, 200);

    // The addresses a static host also answers: a page asked for with its
    // .html, and a directory asked for without its slash.
    if (key.endsWith(".html")) {
      const pretty = key.slice(0, -5).replace(/(^|\/)index$/, "$1");
      if (await lookup(env, m, pretty)) return Response.redirect(new URL("/" + pretty + url.search, url), 301);
    }
    if (key !== "" && !key.endsWith("/") && (await lookup(env, m, key + "/"))) {
      return Response.redirect(new URL("/" + key + "/" + url.search, url), 301);
    }
    const missing = await lookup(env, m, "404");
    if (missing) return serve(request, env, ctx, m, "404", missing, 404);
    return new Response("not found", { status: 404 });
  },
};
