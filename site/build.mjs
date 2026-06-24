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

marked.setOptions({ gfm: true, breaks: false });

// Ordered reference docs. The first (`index.html`, from docs/README.md) is the docs landing map. `href`
// is the output filename under site/docs/; `file` is the source path relative to docs/.
const DOCS = [
  { href: "index.html", file: "README.md", title: "Overview" },
  { href: "dsl-grammar.html", file: "dsl-grammar.md", title: "DSL grammar" },
  { href: "feel-subset.html", file: "feel-subset.md", title: "FEEL subset" },
  { href: "cli.html", file: "cli.md", title: "CLI reference" },
  { href: "http-api.html", file: "http-api.md", title: "HTTP API" },
  { href: "project-mode.html", file: "project-mode.md", title: "Project mode" },
  { href: "ai-authoring.html", file: "ai-authoring.md", title: "AI authoring" },
  { href: "ir-format.html", file: "ir-format.md", title: "IR format" },
  { href: "error-schema.html", file: "error-schema.md", title: "Error schema" },
  { href: "architecture.html", file: "architecture.md", title: "Architecture" },
  { href: "decisions.html", file: "decisions.md", title: "Decisions" },
];

const esc = (s) => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;");

function sidebar(activeHref) {
  return (
    "<h4>Reference</h4>" +
    DOCS.map((d) => `<a href="${d.href}"${d.href === activeHref ? ' class="active"' : ""}>${esc(d.title)}</a>`).join("\n")
  );
}

function page(title, activeHref, contentHtml) {
  return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>feelc docs — ${esc(title)}</title>
  <link rel="stylesheet" href="../style.css" />
</head>
<body>
  <nav class="nav">
    <div class="brand"><a href="../" style="color:inherit">feelc</a><span> · docs</span></div>
    <div class="links">
      <a href="../playground/">Playground</a>
      <a href="https://github.com/maxgfr/feelc" target="_blank" rel="noopener">GitHub</a>
    </div>
  </nav>
  <div class="docs">
    <aside>${sidebar(activeHref)}</aside>
    <div class="content">${contentHtml}</div>
  </div>
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
    writeFileSync(join(docsOut, d.href), page(d.title, d.href, fixLinks(marked.parse(md))));
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

renderDocs();
buildExamples();
checkLinks();
