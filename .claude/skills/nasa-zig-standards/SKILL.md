---
name: nasa-zig-standards
description: Code standards for all Zig written in this repo, based on NASA's Power of 10 rules for safety-critical code. Load before writing or reviewing any Zig code here.
---

# NASA Power of 10 standards for sprue

All Zig code in this repo follows NASA's Power of 10 rules, adapted from C to Zig. These are strict rules, not guidelines. Code that cannot satisfy a rule needs a redesign, not an exemption. The point is code that a reviewer can verify by reading, which suits a public portfolio repo where the code itself is the exhibit.

## The ten rules, as they apply here

1. **Simple control flow.** No recursion anywhere, including mutual recursion. Zig has no goto, so the remaining discipline is: every algorithm is expressed with loops. If a problem feels recursive (walking a directory tree), use an explicit work list with a fixed capacity instead of calling yourself.

2. **Every loop has a fixed upper bound.** Every `while` loop has a compile-time-known maximum iteration count, enforced with a counter and an assertion. `for` loops over slices are inherently bounded and are the preferred form. Where input size is the bound (number of content files), a named limit constant caps it (`max_posts`), and exceeding the cap is a hard error with a clear message, never silent truncation.

3. **No dynamic allocation after startup.** All memory comes from a single fixed-size arena allocated once in `main` before any real work. Downstream code may take memory from that arena but never from the system, nothing frees individually, and every collection has a hard cap so total use can be reasoned about. If the arena runs out the program fails with a clear message rather than growing.

4. **Functions fit on one page.** Sixty lines maximum, and shorter is better. A function that needs more is doing two jobs.

5. **At least two assertions per function.** Use `std.debug.assert` to check preconditions on entry and invariants or postconditions before return. Assertions state what must be true, not what probably is. Trivial functions that genuinely have nothing to assert should be folded into their caller.

6. **Smallest possible scope for all data.** No module-level `var`. Constants are fine at module level. Everything mutable is local to a function or passed explicitly. If two functions need shared state, one passes it to the other.

7. **Check every return value, validate every input.** Zig's error unions make ignoring errors a compile error, so the remaining discipline is: never `catch unreachable` on an error that can actually occur, and never discard a meaningful result with `_ =` without a stated reason the next reader can verify. Every public function validates its inputs with assertions or returns an error for bad input.

8. **No metaprogramming beyond the trivial.** Zig has no preprocessor, so this rule governs `comptime`: use it only for constants and simple type-level plumbing the standard library expects. No comptime code generation, no clever type machinery. If a reviewer has to execute comptime logic in their head, it is too clever.

9. **Single-level indirection.** Slices and single-item pointers only. No pointers to pointers, no function pointers, no callback tables. Data flows through direct calls and return values.

10. **Zero warnings, strictest build.** The build enables all safety checks. `@setRuntimeSafety(false)` and `unreachable`-as-optimization are forbidden. `zig fmt --check` and `zig build test` pass clean before any commit. Unused variables and unreachable code are compile errors in Zig already; nothing may be renamed to `_` merely to silence that, per the naming rules below.

## Naming and style

- Names must sound natural and human when read aloud. No abbreviations unless universal, no names that need decoding.
- Follow standard Zig conventions: `TitleCase` for types, `camelCase` for functions, `snake_case` for variables and constants, file names in `snake_case`.
- Never underscore-prefix or `_`-discard a binding to silence the compiler. An unused value is dead code or a bug: remove it or use it.
- Comments explain why, never what, and never appear as section banners. If code needs a paragraph of explanation, redesign it.
- No em dashes in any text this repo produces: source comments, output HTML, docs, commit messages.

## Architecture rules

- The generator is a single small program: read content files, transform, write HTML. Everything testable lives in library files with no I/O; `main.zig` is a thin shell that owns the arena and the filesystem.
- The least code that solves the problem. No speculative abstraction, no plugin systems, no configuration for situations that do not exist yet.
- Dependencies: none beyond the Zig standard library. That is the whole point.

## Gates

Before any commit, all of these pass clean:

```
zig fmt --check .
zig build test
zig build run
```

Every transformation rule has at least one test with real input and expected HTML output. A function without its two assertions fails review even if it works.
