// Build-time assembler for the GitHub Pages site (run by .github/workflows/pages.yml):
//   1. renders the repo's docs/*.md into themed HTML pages with a sidebar (single source = the
//      markdown in docs/, so the site never drifts from the reference docs);
//   2. generates site/examples.json from examples/*/ (title from spec.md + the .rules text) which
//      the playground loads into its example picker.
// The .wasm module and wasm_exec.js are produced/copied separately by the workflow.
//
// Dependency: `marked` (installed at build time via `npm i --no-save marked`, not committed —
// consistent with the repo's existing build-time npx/Node usage in release.yml).

import { marked } from "marked";
import { readFileSync, writeFileSync, readdirSync, mkdirSync, existsSync, statSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join, basename } from "path";

const siteDir = dirname(fileURLToPath(import.meta.url));
const root = dirname(siteDir);
const docsSrc = join(root, "docs");
const examplesSrc = join(root, "examples");
const docsOut = join(siteDir, "docs");

marked.setOptions({ gfm: true, breaks: false });

// Ordered reference docs (ADRs intentionally excluded). The first one becomes docs/index.html.
const DOCS = [
  { slug: "index", file: "dsl-grammar.md", title: "DSL grammar" },
  { slug: "feel-subset", file: "feel-subset.md", title: "FEEL subset" },
  { slug: "project-mode", file: "project-mode.md", title: "Project mode" },
  { slug: "ir-format", file: "ir-format.md", title: "IR format" },
  { slug: "error-schema", file: "error-schema.md", title: "Error schema" },
];

function sidebar(activeSlug) {
  return DOCS.map((d) => {
    const href = d.slug === "index" ? "index.html" : `${d.slug}.html`;
    const cls = d.slug === activeSlug ? ' class="active"' : "";
    return `<a href="${href}"${cls}>${d.title}</a>`;
  }).join("\n");
}

function page(title, activeSlug, contentHtml) {
  return `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>feelc docs — ${title}</title>
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
    <aside><h4>Reference</h4>${sidebar(activeSlug)}</aside>
    <div class="content">${contentHtml}</div>
  </div>
</body>
</html>
`;
}

function renderDocs() {
  if (!existsSync(docsOut)) mkdirSync(docsOut, { recursive: true });
  for (const d of DOCS) {
    const md = readFileSync(join(docsSrc, d.file), "utf8");
    const html = page(d.title, d.slug, marked.parse(md));
    const out = join(docsOut, d.slug === "index" ? "index.html" : `${d.slug}.html`);
    writeFileSync(out, html);
    console.log("docs:", out);
  }
}

// title pulls the first markdown heading (or first non-empty line) from a spec.md.
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
