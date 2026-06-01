<div align="center">

<img src="../docs/banner.svg" alt="gambit — store any file as a playable game of chess" width="100%" />

# ♟️ gambit · Rust port

### *The same program, in a second language — interoperable down to the byte.*

This is the **+5 second-language port** of [`gambit`](..) (Go → Rust) for **Code Olympics 2026**. It is not a loose re-implementation: it runs the *identical* algorithm, so it emits **byte-for-byte identical PGN** for the same input and **round-trips across languages** — encode with Go, decode with Rust (or vice-versa) and your file returns intact.

<br/>

![Rust](https://img.shields.io/badge/Rust-1.92-CE422B?style=for-the-badge&logo=rust&logoColor=white)
![stdlib only](https://img.shields.io/badge/dependencies-0-1D9E75?style=for-the-badge)
![clippy](https://img.shields.io/badge/clippy-clean-1D9E75?style=for-the-badge)

![tests](https://img.shields.io/badge/tests-6_passing-1D9E75?style=flat-square)
![byte-identical](https://img.shields.io/badge/PGN_vs_Go-byte--identical-1D9E75?style=flat-square)
![cross round-trip](https://img.shields.io/badge/cross--language_round--trip-lossless-1D9E75?style=flat-square)
![short names](https://img.shields.io/badge/var_names_>3_chars-0-1D9E75?style=flat-square)
![lines](https://img.shields.io/badge/code-484_lines-2A6DB2?style=flat-square)

</div>

---

## 🎯 Why this is the strongest possible port

The contest rewards a second-language port (+5) as evidence of **language adaptation**. Most ports prove "I can write the idea twice." gambit can prove something far stronger, because it has **zero randomness, zero floats, zero external dependencies** — its output is a pure function of its input. So the two implementations can be held to the highest bar there is: **bit-for-bit equivalence.**

```powershell
# encode the same file with each implementation
.\gambit-go.exe   secret.bin -o go.pgn
.\gambit-rs.exe   secret.bin -o rs.pgn
fc /b go.pgn rs.pgn          # → no differences

# decode each implementation's PGN with the OTHER one
.\gambit-rs.exe -d go.pgn -o from_go.bin   # Rust reads Go's chess game
.\gambit-go.exe -d rs.pgn -o from_rs.bin   # Go reads Rust's chess game
# from_go.bin == from_rs.bin == secret.bin
```

| Property | Result |
|---|:--:|
| Go PGN vs Rust PGN, same input | **byte-identical** |
| Rust decodes Go's PGN → original bytes | ✅ |
| Go decodes Rust's PGN → original bytes | ✅ |
| Binary executable, Rust-encode → Go-decode | ✅ |

This is only possible because the **reversibility invariant is information-theoretic, not language-specific**: a canonical *total order* on legal moves makes `bits ↔ index ↔ move` a bijection. Re-implement that order faithfully in any language and the games come out the same.

---

## 🥷 The constraint transferred — for free, again

The headline finding of the whole project is that the **Short-Name Ninja** rule (variables ≤ 3 chars) costs almost nothing in Go because short names in tight scopes are *already idiomatic Go*. The port tests that claim against a very different language — and it holds:

- **Every local, parameter and loop variable in the Rust port is ≤ 3 characters** — `i`, `j`, `sq`, `mv`, `pc`, `dr`, `df`, `rd`, `wr`, `sb`, `lm`, `nx`, … — and not one felt forced. Rust favours short names in short scopes just as Go does.
- Clarity is carried, identically, by **descriptive function/type/field names** (`legal_moves`, `BitWriter::write`, `Move::pro`) — which the rule exempts.
- The port is **484 lines of code** — comfortably under the 500-line budget *in Rust too*.
- `cargo clippy` is **clean** (zero warnings); idiomatic-Rust touches like `(0..=7).contains(&f)`, slice patterns in `replace(['-', 'x'], "")`, and `cnt.is_multiple_of(8)` replace their Go phrasings.

> The deeper point for the *Language Adaptation* score: the constraint and these languages **pull in the same direction**. Both Go and Rust locate readability in names that the rule leaves untouched, so a rule that shortens *variables* never fights the language's idea of clarity. See [`../EMERGENT.md`](../EMERGENT.md) for the full write-up.

---

## 🚀 Build & verify

```powershell
cargo build --release          # → target/release/gambit.exe
cargo test                     # 6 tests: round-trip, all-byte-values, multi-game,
cargo clippy                   #          20-move opening, bits(), bad-count rejection
```

```powershell
# the cross-language proof, end to end
cargo build --release
go build -C .. -o gambit-go.exe .
.\target\release\gambit.exe anyfile.bin -o rs.pgn
.\gambit-go.exe -d rs.pgn -o restored.bin
(Get-FileHash anyfile.bin).Hash -eq (Get-FileHash restored.bin).Hash   # → True
```

The test suite mirrors the Go one, and deliberately includes the **two denial-of-service inputs the Go fuzzer discovered** (a huge `[Bytes]` count and a negative one) so the port is hardened against them from birth, not after the fact.

<div align="center">

---

### *Write the bijection in any language; the chess games come out the same.* ♟️

Built for **Code Olympics 2026** · Second-language port (Go → Rust) · stdlib only

</div>
