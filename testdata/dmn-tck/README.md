# DMN TCK subset (fixtures)

Fixtures in the **official DMN TCK format** (`<testCases>`, namespace
`http://www.omg.org/spec/DMN/20160719/testcase`): a `<model>.dmn` pair + one or more
`*-test-*.xml` files. Run by `feelc tck --suite testdata/dmn-tck` (and by
`internal/tck`).

## Provenance

These files are **hand-written** to stay *self-contained* and deterministic (no
network download). They faithfully mimic the schema of the
[official DMN TCK](https://github.com/dmn-tck/tck) (Apache-2.0). The `feelc tck` harness also works
as-is on a **real TCK checkout**: `feelc tck --suite <path-to-tck/TestCases>`.

## Selected cases

- `grade/` — scalar `FIRST` table (score → F/B/A). `grade-test-01.xml`: 3 **passing** cases.
  `grade-test-02-skip.xml`: 1 **skipped** case (expected value of type `date`, outside the subset).

## Families deliberately absent (outside the v2 subset, would be SKIPPED)

- temporal types (`date`, `time`, `dateTime`, `duration`) and `function`;
- multi-decision DRGs whose `informationRequirement`/`requiredDecision` wiring is not imported;
- multi-argument built-ins (cf. ADR 0004 §3).

The `feelc tck` report always distinguishes `passed` / `failed` / `skipped` (with reason):
conformance = `passed / (passed+failed)`, skips do not count toward it (honest coverage).
