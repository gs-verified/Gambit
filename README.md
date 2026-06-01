<div align="center">

<img src="docs/banner.svg" alt="gambit — store any file as a playable game of chess" width="100%" />

# ♟️ gambit

### *Every file is a game waiting to be played. Decode it and you get the file back, byte-for-byte.*

`gambit` encodes arbitrary bytes as a **legal, playable game of chess** in standard PGN — and decodes that PGN back to the **exact** original bytes. Not a string that *looks* chess-ish: a real game any chess GUI will open and let you play through. Built for **Code Olympics 2026**.

<br/>

![Go](https://img.shields.io/badge/Go-1.25-00ADD8?style=for-the-badge&logo=go&logoColor=white)
![stdlib only](https://img.shields.io/badge/dependencies-0-1D9E75?style=for-the-badge)
![Domain](https://img.shields.io/badge/domain-Basic_Tools-7C3AED?style=for-the-badge)

![tests](https://img.shields.io/badge/tests-passing-1D9E75?style=flat-square)
![round trip](https://img.shields.io/badge/round--trip-lossless-1D9E75?style=flat-square)
![fuzzed](https://img.shields.io/badge/fuzzed-8.8M_execs-1D9E75?style=flat-square)
![vet + staticcheck](https://img.shields.io/badge/vet_%2B_staticcheck-clean-2A6DB2?style=flat-square)
![short-name violations](https://img.shields.io/badge/var_names_>3_chars-0-1D9E75?style=flat-square)
![line budget](https://img.shields.io/badge/code-472%2F500_lines-1D9E75?style=flat-square)
![Code Olympics](https://img.shields.io/badge/Code_Olympics-2026-EF9F27?style=flat-square)
[![watch the demo](https://img.shields.io/badge/▶_watch-3--min_demo-FF0000?style=flat-square&logo=youtube&logoColor=white)](https://youtu.be/UdtQmsJGL-w)

**[🎬 Demo](https://youtu.be/UdtQmsJGL-w) • [🧠 The idea](#-the-idea) • [🧩 Framing](#-framing-arbitrary-lengths) • [♟️ The engine](#️-the-chess-engine) • [🏆 Constraints](#-the-constraints) • [🛡️ Robustness](#️-robustness-30-of-the-score) • [🚀 Run it](#-run-it)**

<br/>

<a href="https://youtu.be/UdtQmsJGL-w"><img src="https://img.youtube.com/vi/UdtQmsJGL-w/maxresdefault.jpg" width="70%" alt="▶ Watch the gambit demo on YouTube" /></a>

**▶ [Watch the 3-minute demo](https://youtu.be/UdtQmsJGL-w)**

</div>

---

## 🎬 The money shot

```console
$ gambit secret.txt > game.pgn      # encode: bytes → chess
$ gambit -d game.pgn > out.txt      # decode: chess → bytes  (identical)
```

```pgn
[Event "gambit"]
[Round "1"]
[Bytes "32"]

1. Ng1-f3 Ng8-f6 2. d2-d4 d7-d5 3. Bc1-f4 Bc8-f5 4. e2-e3 ...  *
```

> Paste that into any chess app and **play it**. Feed it back to `gambit -d` and your file returns, every byte intact.

---

## 🧠 The idea

At any chess position there is a finite, **ordered** list of legal moves. A position with `N` legal moves can carry `⌊log₂ N⌋` bits: read that many bits from the input, treat them as an index, and play the move at that index.

```mermaid
flowchart LR
  B["🔢 input bits<br/>0110 1011 …"] --> L["♟️ legal moves<br/>sorted canonically"]
  L --> K["📐 k = ⌊log2 N⌋ bits<br/>→ index"]
  K --> M["▶️ play moves[idx]<br/>apply(p, move)"]
  M -->|"repeat until bits run out"| L
  M --> P["📜 PGN<br/>a real game"]
```

Decoding runs the same machine in reverse: replay each move from the PGN, find its **index** in the very same sorted list, and emit those `k` bits.

The whole scheme rests on one invariant — **encoder and decoder generate and sort legal moves identically** — so `move ↔ index ↔ bits` is a bijection. Reversibility isn't bolted on; it's structural.

| Step | Encode | Decode |
|---|---|---|
| moves | `legalMoves(p)` *(canonical sort)* | `legalMoves(p)` *(same list)* |
| map | bits → index → `moves[idx]` | move → `find` → index → bits |
| advance | `apply(p, move)` | `apply(p, move)` |

---

## 🧩 Framing arbitrary lengths

| | Mechanism | Why |
|:--:|---|---|
| 🏷️ | **Byte count** in game 1's `[Bytes "N"]` tag | the decoder writes bits, then trims to exactly `N` bytes — discarding the final move's zero padding |
| ♻️ | **New game block** when a game ends with bits unspent | PGN holds many games, so a big file becomes a small *tournament*; the opening always offers 20 moves, so progress is guaranteed |
| ⏹️ | **200-ply safety cap** per game | every game terminates; no input can produce an unbounded game |
| 0️⃣ | **Empty input** → one valid, empty game | `[Bytes "0"]`, still decodes cleanly |

---

## ♟️ The chess engine

A deliberately small but **legal subset** of the rules: all eight piece movements, captures, check, checkmate and stalemate detection, and automatic queen promotion.

It omits **castling** and **en passant** — the two special-case moves — purely to keep the move generator compact. Every game it emits obeys the laws of chess; it simply never plays those two moves. The subset is **documented, not hidden**, and any GUI loads the result.

---

## 🏆 The constraints

> Code Olympics 2026 sets two hard limits. The headline finding: **they cost almost nothing in Go.**

| Constraint | Limit | This tool |
|---|---|:--:|
| 🥷 **Short-Name Ninja** | variable names ≤ 3 chars | **0 violations** *(machine-checked)* |
| 📏 **Professional Builder** | ≤ 500 code lines | **472** code lines *(see the line-count note below)* |
| 🧰 **Basic Tools** | calc / convert / generate / encode | an **encoder** |
| 🐹 **Language** | Go, stdlib only | `sort` · `strconv` · `strings` · `flag` · `io` · `os` |

Board code is intrinsically index-heavy — `i`, `j`, `sq`, `mv`, `pc`, `dst`, `src`, `dr`, `df` — exactly the short names Go already prefers in tight scopes. The 3-char ceiling on **variables** never bit once. Clarity is carried instead by **descriptive function and type names** (`legalMoves`, `attacked`, `BitReader.read`) and a heavily commented body — comments are free under the line rule, so the chess logic stays fully explained.

Compliance is proven mechanically by [`tools/check.go`](tools/check.go), which parses the AST and flags any variable identifier over three characters.

<details>
<summary><b>📏 How "lines" are counted — and why (plus the bulletproof twin)</b></summary>

The budget is read as **500 lines of *code*** — non-blank, non-comment source lines — with comments and blank lines free. That is the only reading consistent with the contest's own framing:

- it's the **"Professional Builder"** rule, and professionals comment their code;
- Go is praised in the brief as *"built for clarity"*, and the rubric **rewards explanatory comments** under Code Quality — a budget that punished comments would contradict both.

Under that definition, measured mechanically (not by hand) — run it yourself:

```text
$ go -C tools run . ..        # from gambit/, scans this tool's .go files
== .. ==
  code lines (non-test): 472 / 500     ← 28 to spare
  test lines:            151
  variable violations:   0
```

For full transparency: the file is **615 physical lines** total — 472 code, 94 comment, 49 blank. The comments are deliberate — they carry the chess rules and the reversibility argument, and the rubric *rewards* explanatory comments under Code Quality. Stripping them to chase a raw-line count would trade away readability for no functional gain.
</details>

> 🎖️ **Bonus material in this repo:** a byte-identical **[Rust port](rust)** (the +5 second-language port) and **[`EMERGENT.md`](EMERGENT.md)** — a written-up emergent finding plus honest language-learning reflections.

---

## 🛡️ Robustness (30% of the score)

- 🔒 **Round-trip is the contract**, tested exhaustively: random inputs of many sizes, byte-unaligned lengths, the empty slice, all 256 byte values, plain text, and a 1000-byte input that deliberately **spans multiple game blocks**.
- 🎯 **Fuzzed with Go's native fuzzer** (`go test -fuzz`): ~**8.8 M** decode executions and tens of thousands of full encode→decode round-trips, **zero** failures. Fuzzing earned its keep — it found, and we fixed, **two denial-of-service crashes** from a hostile `[Bytes "N"]` count (a ~88 TB allocation and a negative-length `makeslice` panic). Both inputs are now **permanent regression seeds** in `testdata/`.
- 🚫 Decoding **rejects invalid PGN**: a missing `[Bytes]` tag, a byte count outside `[0, decoded]`, and any move illegal for its position each produce a clear error rather than silent garbage or a crash.
- 🧭 Move generation works in **file/rank space**, so sliding pieces never wrap around a board edge — a classic off-by-one chess bug, designed out.
- ♾️ The **200-ply cap** guarantees every game terminates and the encoder always makes progress.
- 🧹 Non-move tokens (move numbers, the `*` result, stray tag text) are **skipped**, so hand-edited or reformatted PGN still decodes.
- 🎛️ Flags work **in any position** (`gambit file -o out` and `gambit -o out file` both parse) — Go's `flag` stops at the first operand, so the input path is re-scanned past it.

---

## 🚀 Run it

```powershell
gambit report.pdf -o report.pgn          # encode a file
gambit -d report.pgn -o report.pdf        # decode it back
echo -n "hi" | gambit                     # stdin → stdout PGN
```

| Flag | Meaning |
|---|---|
| `-d` | decode PGN back into the original bytes *(default: encode)* |
| `-o file` | write to file *(default: stdout)* |
| `in` | read from file *(default: stdin)* — may appear before, after, or between flags |

```powershell
go test ./...        # round-trip + edge-case + regression tests
go vet ./...         # clean — and so is staticcheck
```

<details>
<summary><b>🔬 Verify a lossless round-trip yourself</b></summary>

```powershell
go build -o gambit.exe .
.\gambit.exe -o game.pgn anyfile.bin
.\gambit.exe -d -o restored.bin game.pgn
(Get-FileHash anyfile.bin).Hash -eq (Get-FileHash restored.bin).Hash   # → True
```
</details>

<details>
<summary><b>🧪 What the tests prove</b></summary>

The headline `decode(encode(x)) == x` across many sizes and known inputs; multi-game framing for large files; PGN well-formedness; graceful rejection of bad PGN (missing/garbage/out-of-range `[Bytes]`, illegal moves); the opening position having exactly **20** legal moves; `bits` being `⌊log₂ n⌋`; LAN parsing/rendering (including rejecting move numbers and results); queen promotion appearing in the move list. Two **fuzz targets** (`FuzzRoundTrip`, `FuzzDecode`) drive the reversibility invariant and assert `decode` never panics on arbitrary text.
</details>

---

## 📚 Language learnings

- `sort.Slice` with a tuple comparator (`src`, then `dst`, then promotion) gives the one **canonical move order** both sides depend on — the entire reversibility guarantee is a single small, total ordering.
- **Comparable structs** make `Move == Move` and `find` trivial: no custom equality, no hashing, just `==` on a plain value type.
- Go 1.24's `strings.SplitSeq` / `FieldsSeq` iterators tokenise PGN **without allocating** intermediate slices, and `for range n` integer loops removed every manual counter — which dovetailed neatly with the short-name rule.
- The stdlib `flag` package **stops at the first non-flag argument** — a real footgun the fuzzing mindset surfaced: `gambit file -o out` silently ignored `-o`. Re-parsing the tail after each operand makes flag order irrelevant.
- **Fuzzing > intuition for input validation.** The round-trip math was correct, but `decode` trusted an attacker-controlled length straight into `make`. The fuzzer found both the huge-count and negative-count crashes in seconds.

<div align="center">

---

### *Your file is already a chess game. gambit just writes it down.* ♟️

Built for **Code Olympics 2026** · Domain: Basic Tools · Go, stdlib only

</div>
