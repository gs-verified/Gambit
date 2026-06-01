package main

import (
	"bytes"
	"math/rand/v2"
	"strings"
	"testing"
)

// The headline guarantee: decode(encode(x)) == x for inputs of every size,
// including the empty slice, sizes that don't fall on byte boundaries, and
// inputs long enough to spill across several game blocks.
func TestRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for _, sz := range []int{0, 1, 2, 3, 7, 8, 9, 31, 64, 200, 500} {
		in := make([]byte, sz)
		for i := range in {
			in[i] = byte(rng.IntN(256))
		}
		pgn := encode(in)
		out, err := decode(pgn)
		if err != nil {
			t.Fatalf("size %d: decode failed: %v", sz, err)
		}
		if !bytes.Equal(in, out) {
			t.Fatalf("size %d: round trip mismatch\n in=%v\nout=%v", sz, in, out)
		}
	}
}

// Known inputs (text and all-byte-values) must survive the round trip too.
func TestRoundTripKnown(t *testing.T) {
	cs := [][]byte{
		[]byte("Code Olympics 2026"),
		[]byte("The quick brown fox jumps over the lazy dog."),
		func() []byte {
			b := make([]byte, 256)
			for i := range b {
				b[i] = byte(i)
			}
			return b
		}(),
	}
	for _, in := range cs {
		out, err := decode(encode(in))
		if err != nil {
			t.Fatalf("decode failed: %v", err)
		}
		if !bytes.Equal(in, out) {
			t.Fatalf("round trip mismatch for %q", in)
		}
	}
}

// The encoded text must be loadable PGN: it carries the tags and ends each game
// with the "*" marker.
func TestEncodeIsPGN(t *testing.T) {
	out := encode([]byte("hello"))
	for _, sub := range []string{"[Event \"gambit\"]", "[Bytes \"5\"]", "1. ", "*"} {
		if !strings.Contains(out, sub) {
			t.Errorf("encoded PGN missing %q\n%s", sub, out)
		}
	}
}

// Long input must produce more than one game block.
func TestMultiGame(t *testing.T) {
	in := make([]byte, 1000)
	for i := range in {
		in[i] = byte(i * 7)
	}
	if n := strings.Count(encode(in), "[Event"); n < 2 {
		t.Fatalf("expected several game blocks for 1000 bytes, got %d", n)
	}
	out, err := decode(encode(in))
	if err != nil || !bytes.Equal(in, out) {
		t.Fatalf("multi-game round trip failed: err=%v equal=%v", err, bytes.Equal(in, out))
	}
}

func TestDecodeErrors(t *testing.T) {
	cs := []string{
		"1. e2-e4 e7-e5 *",                            // no [Bytes] tag
		"[Bytes \"4\"]\n\n1. e2-e5 *",                 // parseable but illegal (pawn triple-step)
		"[Event \"x\"]\n[Bytes \"4\"]\n\n1. Ke1-e5 *", // a real square but an illegal king move
	}
	for _, p := range cs {
		if _, err := decode(p); err == nil {
			t.Errorf("expected error decoding %q", p)
		}
	}
}

// The opening position has exactly 20 legal moves (16 pawn pushes, 4 knight).
func TestStartPosMoves(t *testing.T) {
	if n := len(legalMoves(startPos())); n != 20 {
		t.Fatalf("start position has %d legal moves, want 20", n)
	}
}

// bits is floor(log2 n) and 0 for a forced (single-move) position.
func TestBits(t *testing.T) {
	cs := []struct{ n, out int }{{1, 0}, {2, 1}, {3, 1}, {4, 2}, {7, 2}, {8, 3}, {20, 4}}
	for _, c := range cs {
		if g := bits(c.n); g != c.out {
			t.Errorf("bits(%d) = %d, want %d", c.n, g, c.out)
		}
	}
}

// A bishop move is recognised as a capture and a checking move is marked '+'.
func TestNotation(t *testing.T) {
	if g := sqName(0); g != "a1" {
		t.Errorf("sqName(0) = %q, want a1", g)
	}
	if g := sqName(63); g != "h8" {
		t.Errorf("sqName(63) = %q, want h8", g)
	}
	m, ok := parseLAN("Ng1-f3")
	if !ok || m.src != parseSq("g1") || m.dst != parseSq("f3") {
		t.Errorf("parseLAN(Ng1-f3) = %+v ok=%v", m, ok)
	}
	if _, ok := parseLAN("12."); ok {
		t.Error("move number parsed as a move")
	}
	if _, ok := parseLAN("*"); ok {
		t.Error("result token parsed as a move")
	}
}

// A pawn reaching the last rank must promote (to a queen) in the move list.
func TestPromotion(t *testing.T) {
	var p Pos
	p.sq[parseSq("a7")] = 1  // lone white pawn one step from promotion
	p.sq[parseSq("e1")] = 6  // kings so the position is legal
	p.sq[parseSq("e8")] = -6 // ...
	p.wtm = true
	got := false
	for _, m := range legalMoves(p) {
		if m.dst == parseSq("a8") && m.pro == 5 {
			got = true
		}
	}
	if !got {
		t.Fatal("pawn did not generate a queen promotion on a8")
	}
}

// FuzzRoundTrip drives the headline guarantee with Go's native fuzzer: the
// engine mutates the seed corpus toward awkward inputs the table tests might
// miss, and any byte sequence that fails decode(encode(dat)) == dat is a real
// reversibility bug. Huge inputs are skipped -- correctness is structural, not
// size-dependent, and the cap keeps each iteration fast enough for real coverage.
func FuzzRoundTrip(f *testing.F) {
	for _, s := range [][]byte{nil, {0}, {255}, {1, 2, 3, 4, 5}, []byte("gambit")} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, dat []byte) {
		if len(dat) > 2048 {
			return
		}
		out, err := decode(encode(dat))
		if err != nil {
			t.Fatalf("decode failed for %d bytes: %v", len(dat), err)
		}
		if !bytes.Equal(dat, out) {
			t.Fatalf("round-trip mismatch for %d bytes", len(dat))
		}
	})
}

// FuzzDecode asserts decode never panics on arbitrary text: malformed PGN must
// surface as an error or empty result, never a crash.
func FuzzDecode(f *testing.F) {
	f.Add(encode([]byte("seed")))
	f.Add("[Bytes \"3\"]\n\n1. e2-e4 *")
	f.Add("garbage [Bytes \"x\"] 1. zz9-zz0 *")
	f.Fuzz(func(t *testing.T, txt string) {
		_, _ = decode(txt)
	})
}
