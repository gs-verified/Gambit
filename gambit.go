// Command gambit encodes any file as a legal, playable game of chess, and decodes
// it back to the exact original bytes. It is an *encoder* in the contest's "Basic
// Tools" sense -- it just happens to use the game tree of chess as its alphabet.
//
// The idea: at any chess position there is a finite, ordered list of legal moves.
// A position with N legal moves can carry floor(log2 N) bits -- pick the move
// whose index equals the next bits of the input. Replay the same moves and the
// indices, hence the bits, come back. Both sides generate and sort moves
// identically, so the mapping is perfectly reversible.
//
//	gambit  file        > game.pgn      # encode bytes  -> PGN
//	gambit -d game.pgn  > file          # decode PGN     -> bytes
//
// The byte count is stored in a [Bytes "N"] tag so the decoder knows where the
// data ends (the final move's spare bits are zero padding). Long inputs simply
// continue into fresh game blocks -- a PGN file may hold many games.
//
// The chess engine implements a legal *subset* of the rules: all piece moves,
// captures, check, checkmate and stalemate, with auto-queen promotion, but no
// castling or en passant. Every game it emits is legal and playable; it just
// never uses those two special moves. That keeps the move generator small while
// staying faithful enough that any chess GUI will load the output.
//
// Constraint note (Code Olympics 2026): every *variable* name is <= 3 chars.
// Board code is naturally index-heavy -- i, j, sq, mv, pc -- so the limit cost
// nothing; readability lives in the descriptive function and type names instead.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Move is a single ply: from square src to square dst, promoting to piece code
// pro (0 = no promotion, 5 = queen -- the only promotion this engine makes).
type Move struct {
	src int
	dst int
	pro int8
}

// Pos is a board: sq holds 64 signed piece codes (+white/-black, see pcs), laid
// out a1=0, b1=1 .. h8=63; wtm is true when it is White's turn.
type Pos struct {
	sq  [64]int8
	wtm bool
}

const (
	pcs    = " PNBRQK" // piece letters indexed by code 1..6 (P,N,B,R,Q,K)
	plyCap = 200       // max plies per game block before continuing in a new one
)

// Direction offsets as (file, rank) deltas. Names are <= 3 chars per the rules.
var (
	ort = [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}   // rook lines
	dia = [][2]int{{1, 1}, {1, -1}, {-1, 1}, {-1, -1}} // bishop lines
	knt = [][2]int{{1, 2}, {2, 1}, {2, -1}, {1, -2}, {-1, -2}, {-2, -1}, {-2, 1}, {-1, 2}}
	nbr = [][2]int{ // queen/king: all eight neighbours
		{1, 0}, {-1, 0}, {0, 1}, {0, -1}, {1, 1}, {1, -1}, {-1, 1}, {-1, -1},
	}
)

func main() {
	dec := flag.Bool("d", false, "decode PGN back into the original bytes")
	out := flag.String("o", "", "output file (default: stdout)")
	flag.Parse()

	// flag stops at the first non-flag argument, so a command like
	// "gambit file -o out" would otherwise drop the -o. Re-parse the tail after
	// each operand, letting the input path sit before, after or between flags.
	src := ""
	for len(flag.Args()) > 0 {
		if src == "" {
			src = flag.Arg(0)
		}
		flag.CommandLine.Parse(flag.Args()[1:])
	}

	in := readIn(src)
	if *dec {
		b, err := decode(string(in))
		if err != nil {
			die(err)
		}
		writeOut(*out, b)
		return
	}
	writeOut(*out, []byte(encode(in)))
}

// --- chess: board ---------------------------------------------------------

// startPos returns the standard initial position, White to move.
func startPos() Pos {
	var p Pos
	bak := [8]int8{4, 2, 3, 5, 6, 3, 2, 4} // R N B Q K B N R
	for f := range 8 {
		p.sq[f] = bak[f]     // white back rank (rank 1)
		p.sq[8+f] = 1        // white pawns      (rank 2)
		p.sq[48+f] = -1      // black pawns      (rank 7)
		p.sq[56+f] = -bak[f] // black back rank  (rank 8)
	}
	p.wtm = true
	return p
}

// on returns the square reached by stepping (df,dr) from square i, or -1 if that
// falls off the board. Working in file/rank avoids edge-wrap bugs.
func on(i, df, dr int) int {
	f := i%8 + df
	r := i/8 + dr
	if f < 0 || f > 7 || r < 0 || r > 7 {
		return -1
	}
	return r*8 + f
}

// abs8 is |x| for a piece code.
func abs8(x int8) int8 {
	if x < 0 {
		return -x
	}
	return x
}

// pcOf returns the signed code of piece type t for the given colour (w = white).
func pcOf(t int8, w bool) int8 {
	if w {
		return t
	}
	return -t
}

// --- chess: move generation ----------------------------------------------

// genMoves lists every pseudo-legal move for the side to move (it does not yet
// filter moves that leave one's own king in check -- legalMoves does that).
func genMoves(p Pos) []Move {
	var mv []Move
	for i := range 64 {
		pc := p.sq[i]
		if pc == 0 || (pc > 0) != p.wtm { // empty, or not our piece
			continue
		}
		switch abs8(pc) {
		case 1:
			pawnMoves(p, i, &mv)
		case 2:
			step(p, i, knt, &mv)
		case 3:
			slide(p, i, dia, &mv)
		case 4:
			slide(p, i, ort, &mv)
		case 5:
			slide(p, i, nbr, &mv)
		case 6:
			step(p, i, nbr, &mv)
		}
	}
	return mv
}

// step adds one-square moves (knight, king) along each direction in drs.
func step(p Pos, i int, drs [][2]int, mv *[]Move) {
	for _, d := range drs {
		j := on(i, d[0], d[1])
		if j >= 0 && (p.sq[j] == 0 || (p.sq[j] > 0) != (p.sq[i] > 0)) {
			*mv = append(*mv, Move{i, j, 0})
		}
	}
}

// slide adds sliding moves (bishop, rook, queen) along each direction in drs,
// stopping at the first occupied square (capturing it if it is an enemy).
func slide(p Pos, i int, drs [][2]int, mv *[]Move) {
	for _, d := range drs {
		j := on(i, d[0], d[1])
		for j >= 0 {
			if p.sq[j] == 0 {
				*mv = append(*mv, Move{i, j, 0})
				j = on(j, d[0], d[1])
				continue
			}
			if (p.sq[j] > 0) != (p.sq[i] > 0) {
				*mv = append(*mv, Move{i, j, 0})
			}
			break
		}
	}
}

// pawnMoves adds the pushes and captures for the pawn on square i, promoting to
// a queen on the last rank.
func pawnMoves(p Pos, i int, mv *[]Move) {
	dr, st, ld := 1, 1, 6 // white: forward +1 rank, starts on rank 1, promotes from rank 6
	if p.sq[i] < 0 {
		dr, st, ld = -1, 6, 1 // black mirrors
	}
	one := on(i, 0, dr)
	if one >= 0 && p.sq[one] == 0 {
		addPawn(i, one, i/8 == ld, mv)
		two := on(i, 0, 2*dr)
		if i/8 == st && two >= 0 && p.sq[two] == 0 {
			addPawn(i, two, false, mv)
		}
	}
	for _, df := range []int{-1, 1} {
		cp := on(i, df, dr)
		if cp >= 0 && p.sq[cp] != 0 && (p.sq[cp] > 0) != (p.sq[i] > 0) {
			addPawn(i, cp, i/8 == ld, mv)
		}
	}
}

// addPawn appends a pawn move from a to b, queening when pro is set.
func addPawn(a, b int, pro bool, mv *[]Move) {
	if pro {
		*mv = append(*mv, Move{a, b, 5})
		return
	}
	*mv = append(*mv, Move{a, b, 0})
}

// apply returns the position after move m (no castling/en passant to handle).
func apply(p Pos, m Move) Pos {
	q := p
	pc := q.sq[m.src]
	q.sq[m.src] = 0
	if m.pro != 0 {
		q.sq[m.dst] = pcOf(m.pro, pc > 0)
	} else {
		q.sq[m.dst] = pc
	}
	q.wtm = !q.wtm
	return q
}

// kingSq finds the square of the given colour's king (-1 if somehow absent).
func kingSq(p Pos, w bool) int {
	for i := range 64 {
		if p.sq[i] == pcOf(6, w) {
			return i
		}
	}
	return -1
}

// attacked reports whether square i is attacked by a piece of colour byW.
func attacked(p Pos, i int, byW bool) bool {
	dr := -1 // an attacking white pawn sits one rank below its target
	if !byW {
		dr = 1
	}
	for _, df := range []int{-1, 1} {
		if j := on(i, df, dr); j >= 0 && p.sq[j] == pcOf(1, byW) {
			return true
		}
	}
	for _, d := range knt {
		if j := on(i, d[0], d[1]); j >= 0 && p.sq[j] == pcOf(2, byW) {
			return true
		}
	}
	for _, d := range nbr {
		if j := on(i, d[0], d[1]); j >= 0 && p.sq[j] == pcOf(6, byW) {
			return true
		}
	}
	return ray(p, i, dia, byW, 3) || ray(p, i, ort, byW, 4)
}

// ray reports whether a sliding piece of type t (bishop=3, rook=4) or a queen of
// colour w attacks square i along the given directions.
func ray(p Pos, i int, drs [][2]int, w bool, t int8) bool {
	for _, d := range drs {
		j := on(i, d[0], d[1])
		for j >= 0 {
			if pc := p.sq[j]; pc != 0 {
				if (pc > 0) == w && (abs8(pc) == t || abs8(pc) == 5) {
					return true
				}
				break
			}
			j = on(j, d[0], d[1])
		}
	}
	return false
}

// legalMoves returns the legal moves in p -- pseudo-legal moves minus any that
// leave the mover in check -- sorted into a canonical order. Encoder and decoder
// both call this, so they always see the same list in the same order, which is
// what makes the bit<->move mapping reversible.
func legalMoves(p Pos) []Move {
	var mv []Move
	for _, m := range genMoves(p) {
		nx := apply(p, m)
		if !attacked(nx, kingSq(nx, p.wtm), !p.wtm) {
			mv = append(mv, m)
		}
	}
	sort.Slice(mv, func(a, b int) bool {
		switch {
		case mv[a].src != mv[b].src:
			return mv[a].src < mv[b].src
		case mv[a].dst != mv[b].dst:
			return mv[a].dst < mv[b].dst
		default:
			return mv[a].pro < mv[b].pro
		}
	})
	return mv
}

// find returns the index of move m within mvs, or -1.
func find(mvs []Move, m Move) int {
	for i, x := range mvs {
		if x == m {
			return i
		}
	}
	return -1
}

// --- chess: notation ------------------------------------------------------

// sqName renders a square index as algebraic coordinates, e.g. 0 -> "a1".
func sqName(i int) string {
	return string([]byte{byte('a' + i%8), byte('1' + i/8)})
}

// parseSq parses coordinates like "e4" into a square index, or -1 if malformed.
func parseSq(s string) int {
	if len(s) != 2 || s[0] < 'a' || s[0] > 'h' || s[1] < '1' || s[1] > '8' {
		return -1
	}
	return int(s[1]-'1')*8 + int(s[0]-'a')
}

// lan renders move m in long algebraic notation: [piece]from[-|x]to[=Q][+|#].
func lan(p Pos, m Move) string {
	var sb strings.Builder
	if pc := abs8(p.sq[m.src]); pc != 1 {
		sb.WriteByte(pcs[pc])
	}
	sb.WriteString(sqName(m.src))
	if p.sq[m.dst] != 0 {
		sb.WriteByte('x')
	} else {
		sb.WriteByte('-')
	}
	sb.WriteString(sqName(m.dst))
	if m.pro != 0 {
		sb.WriteByte('=')
		sb.WriteByte(pcs[m.pro])
	}
	nx := apply(p, m)
	if attacked(nx, kingSq(nx, nx.wtm), !nx.wtm) {
		if len(legalMoves(nx)) == 0 {
			sb.WriteByte('#')
		} else {
			sb.WriteByte('+')
		}
	}
	return sb.String()
}

// parseLAN extracts src, dst and promotion from a LAN token, ignoring decorative
// piece letters, capture marks and check marks. ok is false for non-move tokens
// such as move numbers ("12.") and results ("*").
func parseLAN(s string) (Move, bool) {
	s = strings.TrimRight(s, "+#")
	pro := int8(0)
	if i := strings.IndexByte(s, '='); i >= 0 && i+1 < len(s) {
		pro = int8(strings.IndexByte(pcs, s[i+1]))
		s = s[:i]
	}
	if len(s) > 0 && strings.IndexByte("NBRQK", s[0]) >= 0 {
		s = s[1:] // drop a leading piece letter
	}
	s = strings.NewReplacer("-", "", "x", "").Replace(s)
	if len(s) != 4 || pro < 0 {
		return Move{}, false
	}
	a, b := parseSq(s[:2]), parseSq(s[2:])
	if a < 0 || b < 0 {
		return Move{}, false
	}
	return Move{a, b, pro}, true
}

// --- bit streams ----------------------------------------------------------

// BitReader yields bits MSB-first from dat, returning zeros once exhausted.
type BitReader struct {
	dat []byte
	pos int
}

// read consumes the next k bits and returns them as an integer in [0, 2^k).
func (r *BitReader) read(k int) int {
	v := 0
	for range k {
		v <<= 1
		if r.pos < len(r.dat)*8 {
			v |= int(r.dat[r.pos/8]>>(7-r.pos%8)) & 1
		}
		r.pos++
	}
	return v
}

// done reports whether every real bit of the input has been consumed.
func (r *BitReader) done() bool { return r.pos >= len(r.dat)*8 }

// BitWriter packs values MSB-first into a growing byte buffer.
type BitWriter struct {
	buf []byte
	cnt int
}

// write appends the low k bits of v.
func (w *BitWriter) write(v, k int) {
	for n := k - 1; n >= 0; n-- {
		if w.cnt%8 == 0 {
			w.buf = append(w.buf, 0)
		}
		if v>>n&1 == 1 {
			w.buf[w.cnt/8] |= 1 << (7 - w.cnt%8)
		}
		w.cnt++
	}
}

// take returns the first n bytes written, zero-padded if the buffer is short and
// truncated (dropping move-padding bits) if it is long.
func (w *BitWriter) take(n int) []byte {
	out := make([]byte, n)
	copy(out, w.buf)
	return out
}

// bits returns floor(log2 n): the number of choice bits a position with n legal
// moves can carry (0 when there is only one move, so no real choice).
func bits(n int) int {
	k := 0
	for 1<<(k+1) <= n {
		k++
	}
	return k
}

// --- encode / decode ------------------------------------------------------

// encode turns arbitrary bytes into PGN. It walks the game tree, at each position
// reading floor(log2 N) bits and playing the move with that index; when a game
// ends (mate/stalemate or the ply cap) with bits still to spend, it opens a fresh
// game block. The byte count rides in game 1's [Bytes] tag.
func encode(dat []byte) string {
	rd := &BitReader{dat: dat}
	var sb strings.Builder
	gm := 0
	for !rd.done() {
		gm++
		p := startPos()
		var txt []string
		for range plyCap {
			if rd.done() {
				break
			}
			lm := legalMoves(p)
			if len(lm) == 0 {
				break // checkmate or stalemate: this game is over
			}
			idx := 0
			if k := bits(len(lm)); k > 0 {
				idx = rd.read(k)
			}
			txt = append(txt, lan(p, lm[idx]))
			p = apply(p, lm[idx])
		}
		game(&sb, gm, len(dat), txt)
	}
	if gm == 0 { // empty input still yields one valid, empty game
		game(&sb, 1, 0, nil)
	}
	return sb.String()
}

// game writes one PGN game block: a tag header (with [Bytes] on the first game)
// followed by numbered movetext ending in the "*" unfinished-game marker.
func game(sb *strings.Builder, gm, n int, txt []string) {
	fmt.Fprintf(sb, "[Event \"gambit\"]\n[Round \"%d\"]\n", gm)
	if gm == 1 {
		fmt.Fprintf(sb, "[Bytes \"%d\"]\n", n)
	}
	sb.WriteByte('\n')
	for i, s := range txt {
		if i%2 == 0 {
			fmt.Fprintf(sb, "%d. ", i/2+1)
		}
		sb.WriteString(s)
		sb.WriteByte(' ')
	}
	sb.WriteString("*\n\n")
}

// decode reverses encode: it replays every move of every game, recovering each
// move's index and writing those bits, then trims to the stored byte count.
func decode(pgn string) ([]byte, error) {
	n, err := readN(pgn)
	if err != nil {
		return nil, err
	}
	wr := &BitWriter{}
	for _, gt := range games(pgn) {
		p := startPos()
		for tok := range strings.FieldsSeq(text(gt)) {
			m, ok := parseLAN(tok)
			if !ok {
				continue // move number, result, or stray tag fragment
			}
			lm := legalMoves(p)
			ix := find(lm, m)
			if ix < 0 {
				return nil, fmt.Errorf("illegal move %q for this position", tok)
			}
			wr.write(ix, bits(len(lm)))
			p = apply(p, m)
		}
	}
	// A real game always encodes at least n bytes and n is never negative, so a
	// [Bytes] count outside [0, decoded] is a malformed (or hostile) tag -- reject
	// it rather than trusting it into a negative or huge allocation.
	if n < 0 || n > len(wr.buf) {
		return nil, fmt.Errorf("invalid byte count %d for the %d bytes the games encode", n, len(wr.buf))
	}
	return wr.take(n), nil
}

// readN reads the byte count from the [Bytes "N"] tag.
func readN(pgn string) (int, error) {
	i := strings.Index(pgn, "[Bytes \"")
	if i < 0 {
		return 0, fmt.Errorf("not a gambit PGN: no [Bytes] tag")
	}
	i += len("[Bytes \"")
	j := strings.IndexByte(pgn[i:], '"')
	if j < 0 {
		return 0, fmt.Errorf("malformed [Bytes] tag")
	}
	return strconv.Atoi(pgn[i : i+j])
}

// games splits multi-game PGN text into one string per game.
func games(pgn string) []string {
	var gs []string
	for g := range strings.SplitSeq(pgn, "[Event") {
		if strings.TrimSpace(g) != "" {
			gs = append(gs, g)
		}
	}
	return gs
}

// text drops bracketed tag lines from a game, leaving only its movetext.
func text(g string) string {
	var sb strings.Builder
	for ln := range strings.SplitSeq(g, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(ln), "[") {
			sb.WriteString(ln)
			sb.WriteByte(' ')
		}
	}
	return sb.String()
}

// --- cli helpers ----------------------------------------------------------

func die(err error) {
	fmt.Fprintln(os.Stderr, "gambit:", err)
	os.Exit(1)
}

// readIn reads the named file, or stdin when arg is empty.
func readIn(arg string) []byte {
	var b []byte
	var err error
	if arg == "" {
		b, err = io.ReadAll(os.Stdin)
	} else {
		b, err = os.ReadFile(arg)
	}
	if err != nil {
		die(err)
	}
	return b
}

// writeOut writes b to the named file, or stdout when pth is empty.
func writeOut(pth string, b []byte) {
	if pth == "" {
		os.Stdout.Write(b)
		return
	}
	if err := os.WriteFile(pth, b, 0o644); err != nil {
		die(err)
	}
}
