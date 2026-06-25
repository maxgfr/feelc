// Build-time assembler for the GitHub Pages site (run by .github/workflows/pages.yml):
//   1. renders the repo's reference docs (docs/*.md) into themed HTML pages with a sidebar — single
//      source = the markdown in docs/, so the site never drifts from the repo;
//   2. generates site/examples.json from examples/*/ (title from spec.md + the .rules text) which
//      the playground loads into its example picker;
//   3. checks that every relative link in the generated docs resolves (fails the build otherwise).
// The raw ADRs in docs/adr/*.md are NOT rendered as site pages — docs/decisions.md is the one-page
// summary; ADR links resolve to the repo on GitHub. The .wasm module is built separately by the workflow.
//
// Dependency: `marked` (installed at build time via `npm i --no-save marked`, not committed).

import { marked } from "marked";
import { readFileSync, writeFileSync, readdirSync, mkdirSync, existsSync, statSync, rmSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join, basename, resolve } from "path";

const siteDir = dirname(fileURLToPath(import.meta.url));
const root = dirname(siteDir);
const docsSrc = join(root, "docs");
const examplesSrc = join(root, "examples");
const docsOut = join(siteDir, "docs");
const GH = "https://github.com/maxgfr/feelc";

// Shared chrome injected into every generated page (and mirrored verbatim in the static
// site/index.html + site/playground/index.html so all surfaces share one identity + theme).
const WORDMARK =
  '<svg class="wordmark" viewBox="0 0 24 24" width="20" height="20" role="img" aria-label="feelc logo">' +
  '<rect class="mk-bg" x="2" y="2" width="20" height="20" rx="5"/>' +
  '<path class="mk-ck" d="M7 12.4l3.2 3.2L17 8.8" fill="none" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/></svg>';
const THEME_INIT =
  `<script>(function(){var t;try{var q=new URLSearchParams(location.search).get('theme');` +
  `t=(q==='light'||q==='dark')?q:localStorage.getItem('feelc.theme');}catch(e){}` +
  `if(t!=='light'&&t!=='dark')t=matchMedia('(prefers-color-scheme: light)').matches?'light':'dark';` +
  `document.documentElement.dataset.theme=t;})();</script>`;
const THEME_TOGGLE =
  `<button id="theme-toggle" class="theme-toggle" type="button" aria-label="Toggle light or dark theme" title="Toggle theme">` +
  `<span class="icon-moon" aria-hidden="true">☾</span><span class="icon-sun" aria-hidden="true">☀</span></button>`;
const THEME_SCRIPT =
  `<script>(function(){var t=document.getElementById('theme-toggle');if(t)t.addEventListener('click',function(){` +
  `var d=document.documentElement,n=d.dataset.theme==='light'?'dark':'light';d.dataset.theme=n;` +
  `try{localStorage.setItem('feelc.theme',n);}catch(e){}});` +
  `var m=document.getElementById('docs-nav-toggle');if(m)m.addEventListener('click',function(){` +
  `var x=document.body.classList.toggle('nav-open');m.setAttribute('aria-expanded',String(x));});})();</script>`;

marked.setOptions({ gfm: true, breaks: false });

// Ordered reference docs. The first (`index.html`, from docs/README.md) is the docs landing map. `href`
// is the output filename under site/docs/; `file` is the source path relative to docs/.
const DOCS = [
  { href: "index.html", file: "README.md", title: "Overview" },
  { href: "dsl-grammar.html", file: "dsl-grammar.md", title: "DSL grammar" },
  { href: "feel-subset.html", file: "feel-subset.md", title: "FEEL subset" },
  { href: "cli.html", file: "cli.md", title: "CLI reference" },
  { href: "http-api.html", file: "http-api.md", title: "HTTP API" },
  { href: "embedding.html", file: "embedding.md", title: "Embed in your app" },
  { href: "project-mode.html", file: "project-mode.md", title: "Project mode" },
  { href: "ai-authoring.html", file: "ai-authoring.md", title: "AI authoring" },
  { href: "mcp.html", file: "mcp.md", title: "MCP server" },
  { href: "ir-format.html", file: "ir-format.md", title: "IR format" },
  { href: "error-schema.html", file: "error-schema.md", title: "Error schema" },
  { href: "architecture.html", file: "architecture.md", title: "Architecture" },
  { href: "comparison.html", file: "comparison.md", title: "Comparison & gaps" },
  { href: "conformance.html", file: "conformance.md", title: "Conformance" },
  { href: "benchmarks.html", file: "benchmarks.md", title: "Benchmarks" },
  { href: "competitive-report.html", file: "competitive-report.md", title: "Competitive benchmark" },
  { href: "environments.html", file: "environments.md", title: "Environment matrix" },
  { href: "decisions.html", file: "decisions.md", title: "Decisions" },
];

const esc = (s) => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

function sidebar(activeHref) {
  return (
    "<h4>Reference</h4>" +
    DOCS.map((d) => `<a href="${d.href}"${d.href === activeHref ? ' class="active" aria-current="page"' : ""}>${esc(d.title)}</a>`).join("\n")
  );
}

function page(title, activeHref, contentHtml) {
  return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>feelc docs — ${esc(title)}</title>
  ${THEME_INIT}
  <link rel="stylesheet" href="../theme.css" />
  <link rel="stylesheet" href="../style.css" />
</head>
<body>
  <a class="skip-link" href="#content">Skip to content</a>
  <nav class="nav">
    <button id="docs-nav-toggle" class="docs-nav-toggle" type="button" aria-label="Toggle navigation" aria-expanded="false" aria-controls="docs-aside">☰</button>
    <a class="brand" href="../">${WORDMARK} feelc<span> · docs</span></a>
    <div class="links">
      <a href="../playground/">Playground</a>
      <a href="${GH}" target="_blank" rel="noopener">GitHub</a>
      ${THEME_TOGGLE}
    </div>
  </nav>
  <div class="docs">
    <aside id="docs-aside">${sidebar(activeHref)}</aside>
    <main class="content" id="content">${contentHtml}</main>
  </div>
  ${THEME_SCRIPT}
</body>
</html>
`;
}

// fixLinks rewrites relative .md links so cross-references resolve on the site: ADR links and links that
// escape docs/ (root README/CONTRIBUTING/…) go to GitHub; docs/README.md → index.html; siblings → .html.
function fixLinks(html) {
  return html.replace(/href="(?!https?:\/\/|#|mailto:)([^"#]+?)\.md(#[^"]*)?"/g, (_m, path, frag) => {
    frag = frag || "";
    const segs = path.split("/");
    const base = segs[segs.length - 1];
    if (segs.includes("adr")) return `href="${GH}/blob/main/docs/adr/${base}.md${frag}"`;
    if (base === "README" && path.includes("../")) return `href="${GH}#readme"`;
    if (path.includes("../")) return `href="${GH}/blob/main/${base}.md${frag}"`;
    if (base === "README") return `href="index.html${frag}"`;
    return `href="${base}.html${frag}"`;
  });
}

function renderDocs() {
  rmSync(docsOut, { recursive: true, force: true }); // drop any stale output (e.g. old per-ADR pages)
  mkdirSync(docsOut, { recursive: true });
  for (const d of DOCS) {
    const md = readFileSync(join(docsSrc, d.file), "utf8");
    // a11y: marked emits bare <th> (column headers) — add scope="col" for cell↔header association.
    const html = fixLinks(marked.parse(md)).replace(/<th(\s|>)/g, '<th scope="col"$1');
    writeFileSync(join(docsOut, d.href), page(d.title, d.href, html));
    console.log("docs:", d.href);
  }
}

// checkLinks walks the generated docs and fails the build on any relative href whose target is missing.
function checkLinks() {
  const broken = [];
  const walk = (dir) => {
    for (const e of readdirSync(dir, { withFileTypes: true })) {
      const p = join(dir, e.name);
      if (e.isDirectory()) {
        walk(p);
        continue;
      }
      if (!e.name.endsWith(".html")) continue;
      const html = readFileSync(p, "utf8");
      for (const m of html.matchAll(/href="(?!https?:\/\/|#|mailto:)([^"#]+?)(#[^"]*)?"/g)) {
        let target = m[1];
        if (target.endsWith("/")) target += "index.html";
        if (!existsSync(resolve(dirname(p), target))) broken.push(`${p} → ${m[1]}`);
      }
    }
  };
  walk(docsOut);
  if (broken.length) throw new Error(`broken relative link(s) in generated docs:\n  ${broken.join("\n  ")}`);
  console.log("link check: ok");
}

// title pulls the first markdown heading (or first non-empty line) from a markdown file.
function titleFrom(specPath, fallback) {
  if (!existsSync(specPath)) return fallback;
  for (const line of readFileSync(specPath, "utf8").split("\n")) {
    const t = line.trim();
    if (!t) continue;
    return t.replace(/^#+\s*/, "");
  }
  return fallback;
}

function buildExamples() {
  const out = [];
  for (const name of readdirSync(examplesSrc).sort()) {
    const dir = join(examplesSrc, name);
    if (!statSync(dir).isDirectory()) continue;
    const rulesFile = readdirSync(dir).find((f) => f.endsWith(".rules"));
    if (!rulesFile) continue;
    out.push({
      name,
      title: titleFrom(join(dir, "spec.md"), name),
      file: basename(rulesFile),
      rules: readFileSync(join(dir, rulesFile), "utf8"),
    });
  }
  writeFileSync(join(siteDir, "examples.json"), JSON.stringify(out, null, 2));
  console.log(`examples: ${out.length} → site/examples.json`);
}

// copyShared mirrors the single source of truth for the reactive renderers (the embedded serve --ui's
// shared.js) into the playground, so the WASM playground and `feelc serve --ui` can never drift.
function copyShared() {
  const src = join(root, "internal", "service", "web", "shared.js");
  const dst = join(siteDir, "playground", "shared.js");
  writeFileSync(dst, readFileSync(src, "utf8"));
  console.log("shared: internal/service/web/shared.js -> site/playground/shared.js");
}

// copyTheme mirrors the design-system stylesheet (tokens + base + shared components, the single source
// of truth in the embedded serve --ui) into the site, so the playground and docs share one identity and
// the light/dark palette is defined exactly once — same anti-drift contract as copyShared().
function copyTheme() {
  const src = join(root, "internal", "service", "web", "theme.css");
  const dst = join(siteDir, "theme.css");
  writeFileSync(dst, readFileSync(src, "utf8"));
  console.log("theme: internal/service/web/theme.css -> site/theme.css");
}

renderDocs();
copyShared();
copyTheme();
buildExamples();
checkLinks();
