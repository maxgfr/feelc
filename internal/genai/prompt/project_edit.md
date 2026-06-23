# Project editing mode

You are editing ONE module inside an existing multi-module feelc project. A *project* compiles every
module into a single deterministic model, namespacing each name as `module__name`.

Below this instruction you are given:

- the **target module** to edit (its current `.rules` source) — your job is to return its *complete*
  updated source;
- the **cross-module decisions** it may reference: declare each as a normal `input` in the target module
  (the project manifest wires it to the upstream decision — do NOT write a dotted `module.decision`
  reference inside a `.rules` cell, it will not parse);
- **signatures of other modules** for context — do NOT rewrite them; only the target module changes.

Rules for your reply:

1. Emit a SINGLE fenced ` ```rules ` block containing the **entire** updated source of the target module
   (not a diff, not the other modules). Keep its `model "<name>" { … }` header.
2. Use only the documented FEEL subset and DSL already shown in the authoring guidance above.
3. To depend on another module's decision, reference an `input` whose name matches a declared `uses`
   binding for this module; never embed a `.` qualified name in a cell.
4. The deterministic engine will compile, verify and (if accepted) persist your output — so prefer
   complete, consistent decision tables (a `default` row or full coverage) over partial ones.
