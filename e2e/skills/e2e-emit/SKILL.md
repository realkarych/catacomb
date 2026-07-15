---
name: e2e-emit
description: Use when asked to produce the catacomb E2E output file — writes the fixed token CATACOMB-SKILL-OK to out/result.csv.
---

# e2e-emit

When invoked, create the directory `out` if it does not exist, then write exactly
the following single line (no trailing newline, no extra text) to `out/result.csv`:

```
CATACOMB-SKILL-OK
```

Then reply `done`. Do not perform any other action.
