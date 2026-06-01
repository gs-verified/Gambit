# Emergent patterns — Code Olympics 2026

> One tool, two languages, one constraint. This is what fell out — written up because the patterns were *discovered*, not designed in.

The brief sets two hard limits (variable names ≤ 3 chars; ≤ 500 lines) on a tool in the "Basic Tools" domain, written in Go. My entry is [`gambit`](README.md) — it encodes any file as a legal, playable game of chess and decodes it back byte-for-byte. To test whether the constraints were really as cheap as they first looked, I then ported gambit to a second language ([Rust](rust)) under the exact same rules. The entry plus a controlled, byte-identical re-implementation turned out to be enough to see three things clearly.

---

## Pattern 1 — A constraint that agrees with a language's idioms is nearly free

The "Short-Name Ninja" rule sounds punishing: **no variable name longer than three characters.** In many languages it would be. In Go it is almost a no-op — and a controlled re-implementation in Rust shows the cheapness is not a Go fluke:

> **A constraint costs in proportion to how much it fights the language's existing idioms.** The short-name rule barely fights Go *or* Rust, because both locate readability in *names the rule exempts*.

Go's own guidance asks for **short names in short scopes** — `i`, not `index`. The rule simply hardens a soft convention. Measured mechanically by [`tools/check.go`](tools/check.go) (an AST scanner, not eyeballing):

| Build | Language | Variable violations | Code lines | The names it wanted anyway |
|---|---|:--:|:--:|---|
| `gambit` | Go | **0** | 472 | `i j sq mv pc dst src dr df rd wr lm nx` |
| `gambit` | Rust *(the +5 port)* | **0** | 484 | `i j sq mv pc dr df rd wr sb lm nx` |

Two idiomatically very different languages — different type systems, ownership models and standard libraries — produced the **same** result: zero violations, zero contortions, both comfortably under the 500-line budget. Not one name was bent to fit; they are the names an experienced author writes anyway.

And `gambit` is not monolithic. Inside one 472-line file it does **board geometry**, **bit-level stream I/O**, **PGN text parsing**, and **CLI plumbing** — four quite different kinds of code. The rule was invisible across all of them, because in both languages meaning is carried by **function, type and field names** (`legalMoves`, `BitReader.read`, `Move.pro`) — which the rule exempts.

That is the emergent insight: the rule and these languages **pull in the same direction.** A language whose tagline is *"built for clarity"* earns its clarity from descriptive *callable* and *type* names, not long locals — so a rule that shortens locals removes nothing.

**Corollary (cost is asymmetric):** the same rule applied to a language without rich naming elsewhere, or to code that isn't index-heavy, *would* hurt. The cheapness here is the product of constraint-meets-idiom — which is exactly why it's a pattern worth naming rather than a platitude. The Rust port is the **control** that turns "it was cheap for me, once" into "it is cheap wherever the language already prefers short locals."

---

## Pattern 2 — Reliability lives at the trust boundary, not in the core algorithm

`gambit`'s correctness rests on one invariant — encoder and decoder generate and sort legal moves identically, making `bits ↔ index ↔ move` a bijection. That invariant is **provably, exhaustively sound**: table tests across every size and byte value, and Go-native fuzzing with **~8.8 million** decode executions and tens of thousands of full round-trips, found **zero** failures of the round-trip property.

And yet fuzzing immediately found **two crashes.** Neither was in the algorithm. Both were at the **trust boundary**, where the attacker-controlled `[Bytes "N"]` header met a raw allocation:

- `[Bytes "0000000088000000000000"]` → `make([]byte, 88_000_000_000_000)` → out-of-memory.
- `[Bytes "-1"]` → `make([]byte, -1)` → `makeslice` panic.

The lesson that emerged, stated generally:

> **A correct algorithm is not a robust program.** Bugs cluster where untrusted input is turned into a resource decision (a size, a count, an index) — not in the logic the author spent their attention on. Exhaustive *positive* testing (does it round-trip?) is structurally blind to these; only *adversarial* input generation finds them.

The fix was three lines — reject any count outside `[0, decoded]` — but the *finding* is the point: I had written exhaustive round-trip tests and still missed both, because I was testing the part I understood. The fuzzer tested the part I trusted. Both inputs are now permanent regression seeds, and the Rust port was born immune to them.

---

## Pattern 3 — The interesting structure is language-independent

The Go and Rust builds of `gambit` produce **byte-for-byte identical PGN** and decode each other's output losslessly (verified by hash and cross-build round-trip, including on a binary executable). That interoperability is not a coincidence of careful porting — it is forced by the nature of the idea:

> The reversibility isn't a Go feature; it's **information theory.** A canonical *total order* over legal moves is what makes the bit-to-move map a bijection. Implement that same order in any language and the games are necessarily the same.

So both halves of this project are portable: the **idea** (a deterministic, total-order encoding) and the **discipline** (short names, small surface). The constraint didn't trap the solution in one language; the solution is a property of the problem.

---

## Honest language-learning reflections

*What the constraints actually taught me — including the parts that bit.*

**Go**
- `for range n` integer loops and `strings.SplitSeq` / `FieldsSeq` iterators removed nearly every manual counter and intermediate slice — which *dovetailed* with the short-name rule: there was often no counter left to name.
- A **comparable struct** (`Move`) gave `==` and linear search for free — no `Equals`, no hashing.
- The footgun I didn't know I had: `flag` **stops parsing at the first non-flag argument**, so `gambit file -o out` silently ignored `-o`. I only caught it while building the cross-language test harness. Re-parsing the tail after each operand fixed it. (A documentation example that doesn't actually run is worse than no example — fuzzing-mindset paranoia would have caught this earlier.)

**Rust (the port)**
- Translating made Go's *implicit* choices *explicit*: Go silently copies a `Pos` by value; Rust made me say `#[derive(Clone, Copy)]` and `*p`. Go returns `(value, error)`; Rust's `Result<_, String>` with `?` was cleaner for `decode`.
- `&mut Vec<Move>` accumulators ported the Go `*[]Move` pattern almost verbatim — the ownership model accepted the design without complaint.
- The genuinely *new* idioms clippy taught me: `(0..=7).contains(&f)` for range checks, slice patterns (`replace(['-', 'x'], "")`, `trim_end_matches(['+', '#'])`), and `usize::is_multiple_of`.
- **The bug Rust caught that Go never would have:** my Rust test generated data with `i as u32 * 2654435761`, which **overflows `u32`** — and Rust's debug builds *panic on overflow*, so the test failed instantly. The equivalent Go (`int` is 64-bit) would have silently wrapped and quietly passed. Different languages make different mistakes loud; porting surfaced one for free.

**What I'd do differently:** validate every externally-supplied length *at the point it's read*, before any allocation — as a reflex, not as a fix. The round-trip math was the easy 90%; the trust boundary was the 10% that actually crashes.
