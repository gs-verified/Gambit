//! gambit (Rust port) -- encode any file as a legal, playable game of chess, and
//! decode it back to the exact original bytes. This is the **second-language
//! port** of the Go original (../gambit), kept faithful enough that it produces
//! *byte-identical* PGN for the same input and round-trips across languages:
//! decode either implementation's output with the other and the bytes match.
//!
//! The algorithm is unchanged: at each position the legal moves are listed and
//! sorted into one canonical order; a position with N legal moves carries
//! floor(log2 N) bits, chosen as the index of the move to play. Replaying the
//! moves recovers the indices, hence the bits. The byte count rides in a
//! [Bytes "N"] tag; long inputs spill into fresh game blocks.
//!
//! Constraint note (Code Olympics 2026): the contest's <=3-character variable
//! rule is honoured here too -- every local, parameter and loop variable is at
//! most three characters. The point of the port is to show the discipline is
//! not a Go quirk: idiomatic Rust likes short names in tight scopes just as much
//! (i, j, sq, mv, pc, dr, df), so the limit again costs nothing while functions,
//! types and fields stay descriptive.

use std::io::{Read, Write};

const PLY_CAP: i32 = 200; // max plies per game block before continuing in a new one
const PCS: &[u8] = b" PNBRQK"; // piece letters indexed by code 1..6 (P,N,B,R,Q,K)

// Direction offsets as (file, rank) deltas. Const names are exempt from the rule.
const ORT: [[i32; 2]; 4] = [[1, 0], [-1, 0], [0, 1], [0, -1]]; // rook lines
const DIA: [[i32; 2]; 4] = [[1, 1], [1, -1], [-1, 1], [-1, -1]]; // bishop lines
const KNT: [[i32; 2]; 8] = [[1, 2], [2, 1], [2, -1], [1, -2], [-1, -2], [-2, -1], [-2, 1], [-1, 2]];
const NBR: [[i32; 2]; 8] = [[1, 0], [-1, 0], [0, 1], [0, -1], [1, 1], [1, -1], [-1, 1], [-1, -1]];

/// A single ply: from square `src` to `dst`, promoting to piece code `pro`
/// (0 = none, 5 = queen -- the only promotion this engine makes).
#[derive(Clone, Copy, PartialEq, Eq)]
struct Move {
    src: i32,
    dst: i32,
    pro: i8,
}

/// A board: `sq` holds 64 signed piece codes (+white/-black, see PCS), laid out
/// a1=0, b1=1 .. h8=63; `wtm` is true when it is White's turn.
#[derive(Clone, Copy)]
struct Pos {
    sq: [i8; 64],
    wtm: bool,
}

fn main() {
    let arg: Vec<String> = std::env::args().skip(1).collect();
    let mut dec = false;
    let mut out = String::new();
    let mut src = String::new();
    let mut k = 0;
    // Flags may sit before, after or between operands; the lone non-flag arg is
    // the input path. -o consumes the following token as its value.
    while k < arg.len() {
        match arg[k].as_str() {
            "-d" => dec = true,
            "-o" => {
                k += 1;
                if k < arg.len() {
                    out = arg[k].clone();
                }
            }
            s => {
                if src.is_empty() {
                    src = s.to_string();
                }
            }
        }
        k += 1;
    }

    let inp = read_in(&src);
    if dec {
        let txt = String::from_utf8_lossy(&inp);
        match decode(&txt) {
            Ok(b) => write_out(&out, &b),
            Err(e) => die(&e),
        }
    } else {
        write_out(&out, encode(&inp).as_bytes());
    }
}

// --- chess: board ---------------------------------------------------------

/// The standard initial position, White to move.
fn start_pos() -> Pos {
    let mut p = Pos { sq: [0; 64], wtm: true };
    let bak: [i8; 8] = [4, 2, 3, 5, 6, 3, 2, 4]; // R N B Q K B N R
    for (f, &v) in bak.iter().enumerate() {
        p.sq[f] = v; // white back rank (rank 1)
        p.sq[8 + f] = 1; // white pawns      (rank 2)
        p.sq[48 + f] = -1; // black pawns      (rank 7)
        p.sq[56 + f] = -v; // black back rank  (rank 8)
    }
    p
}

/// The square reached by stepping (df,dr) from square `i`, or -1 if it falls off
/// the board. Working in file/rank space avoids edge-wrap bugs.
fn on(i: i32, df: i32, dr: i32) -> i32 {
    let f = i % 8 + df;
    let r = i / 8 + dr;
    if !(0..=7).contains(&f) || !(0..=7).contains(&r) {
        return -1;
    }
    r * 8 + f
}

/// |x| for a piece code.
fn abs8(x: i8) -> i8 {
    if x < 0 {
        -x
    } else {
        x
    }
}

/// The signed code of piece type `t` for the given colour (`w` = white).
fn pc_of(t: i8, w: bool) -> i8 {
    if w {
        t
    } else {
        -t
    }
}

// --- chess: move generation ----------------------------------------------

/// Every pseudo-legal move for the side to move (check filtering happens in
/// `legal_moves`).
fn gen_moves(p: &Pos) -> Vec<Move> {
    let mut mv: Vec<Move> = Vec::new();
    for i in 0i32..64 {
        let pc = p.sq[i as usize];
        if pc == 0 || (pc > 0) != p.wtm {
            continue; // empty, or not our piece
        }
        match abs8(pc) {
            1 => pawn_moves(p, i, &mut mv),
            2 => step(p, i, &KNT, &mut mv),
            3 => slide(p, i, &DIA, &mut mv),
            4 => slide(p, i, &ORT, &mut mv),
            5 => slide(p, i, &NBR, &mut mv),
            6 => step(p, i, &NBR, &mut mv),
            _ => {}
        }
    }
    mv
}

/// One-square moves (knight, king) along each direction in `drs`.
fn step(p: &Pos, i: i32, drs: &[[i32; 2]], mv: &mut Vec<Move>) {
    for d in drs {
        let j = on(i, d[0], d[1]);
        if j >= 0 && (p.sq[j as usize] == 0 || (p.sq[j as usize] > 0) != (p.sq[i as usize] > 0)) {
            mv.push(Move { src: i, dst: j, pro: 0 });
        }
    }
}

/// Sliding moves (bishop, rook, queen) along each direction in `drs`, stopping at
/// the first occupied square (capturing it if it is an enemy).
fn slide(p: &Pos, i: i32, drs: &[[i32; 2]], mv: &mut Vec<Move>) {
    for d in drs {
        let mut j = on(i, d[0], d[1]);
        while j >= 0 {
            if p.sq[j as usize] == 0 {
                mv.push(Move { src: i, dst: j, pro: 0 });
                j = on(j, d[0], d[1]);
                continue;
            }
            if (p.sq[j as usize] > 0) != (p.sq[i as usize] > 0) {
                mv.push(Move { src: i, dst: j, pro: 0 });
            }
            break;
        }
    }
}

/// Pawn pushes and captures for the pawn on square `i`, queening on the last rank.
fn pawn_moves(p: &Pos, i: i32, mv: &mut Vec<Move>) {
    let (mut dr, mut st, mut ld) = (1, 1, 6); // white: forward +1, starts rank 1, promotes rank 6
    if p.sq[i as usize] < 0 {
        dr = -1; // black mirrors
        st = 6;
        ld = 1;
    }
    let one = on(i, 0, dr);
    if one >= 0 && p.sq[one as usize] == 0 {
        add_pawn(i, one, i / 8 == ld, mv);
        let two = on(i, 0, 2 * dr);
        if i / 8 == st && two >= 0 && p.sq[two as usize] == 0 {
            add_pawn(i, two, false, mv);
        }
    }
    for df in [-1, 1] {
        let cp = on(i, df, dr);
        if cp >= 0 && p.sq[cp as usize] != 0 && (p.sq[cp as usize] > 0) != (p.sq[i as usize] > 0) {
            add_pawn(i, cp, i / 8 == ld, mv);
        }
    }
}

/// Append a pawn move from `a` to `b`, queening when `pro` is set.
fn add_pawn(a: i32, b: i32, pro: bool, mv: &mut Vec<Move>) {
    if pro {
        mv.push(Move { src: a, dst: b, pro: 5 });
        return;
    }
    mv.push(Move { src: a, dst: b, pro: 0 });
}

/// The position after move `m` (no castling/en passant to handle).
fn apply(p: &Pos, m: Move) -> Pos {
    let mut q = *p;
    let pc = q.sq[m.src as usize];
    q.sq[m.src as usize] = 0;
    if m.pro != 0 {
        q.sq[m.dst as usize] = pc_of(m.pro, pc > 0);
    } else {
        q.sq[m.dst as usize] = pc;
    }
    q.wtm = !q.wtm;
    q
}

/// The square of the given colour's king (-1 if somehow absent).
fn king_sq(p: &Pos, w: bool) -> i32 {
    for i in 0i32..64 {
        if p.sq[i as usize] == pc_of(6, w) {
            return i;
        }
    }
    -1
}

/// Whether square `i` is attacked by a piece of colour `byw`.
fn attacked(p: &Pos, i: i32, byw: bool) -> bool {
    let mut dr = -1; // an attacking white pawn sits one rank below its target
    if !byw {
        dr = 1;
    }
    for df in [-1, 1] {
        let j = on(i, df, dr);
        if j >= 0 && p.sq[j as usize] == pc_of(1, byw) {
            return true;
        }
    }
    for d in &KNT {
        let j = on(i, d[0], d[1]);
        if j >= 0 && p.sq[j as usize] == pc_of(2, byw) {
            return true;
        }
    }
    for d in &NBR {
        let j = on(i, d[0], d[1]);
        if j >= 0 && p.sq[j as usize] == pc_of(6, byw) {
            return true;
        }
    }
    ray(p, i, &DIA, byw, 3) || ray(p, i, &ORT, byw, 4)
}

/// Whether a sliding piece of type `t` (bishop=3, rook=4) or a queen of colour
/// `w` attacks square `i` along the given directions.
fn ray(p: &Pos, i: i32, drs: &[[i32; 2]], w: bool, t: i8) -> bool {
    for d in drs {
        let mut j = on(i, d[0], d[1]);
        while j >= 0 {
            let pc = p.sq[j as usize];
            if pc != 0 {
                if (pc > 0) == w && (abs8(pc) == t || abs8(pc) == 5) {
                    return true;
                }
                break;
            }
            j = on(j, d[0], d[1]);
        }
    }
    false
}

/// The legal moves in `p` -- pseudo-legal minus any that leave the mover in check
/// -- sorted into one canonical order. Encoder and decoder both call this, so
/// they always see the same list in the same order: that is what makes the
/// bit<->move mapping reversible.
fn legal_moves(p: &Pos) -> Vec<Move> {
    let mut mv: Vec<Move> = Vec::new();
    for m in gen_moves(p) {
        let nx = apply(p, m);
        if !attacked(&nx, king_sq(&nx, p.wtm), !p.wtm) {
            mv.push(m);
        }
    }
    mv.sort_by(|a, b| a.src.cmp(&b.src).then(a.dst.cmp(&b.dst)).then(a.pro.cmp(&b.pro)));
    mv
}

/// The index of move `m` within `mvs`, or -1.
fn find(mvs: &[Move], m: Move) -> i32 {
    for (i, x) in mvs.iter().enumerate() {
        if *x == m {
            return i as i32;
        }
    }
    -1
}

// --- chess: notation ------------------------------------------------------

/// Render a square index as algebraic coordinates, e.g. 0 -> "a1".
fn sq_name(i: i32) -> String {
    let mut s = String::new();
    s.push((b'a' + (i % 8) as u8) as char);
    s.push((b'1' + (i / 8) as u8) as char);
    s
}

/// Parse coordinates like "e4" into a square index, or -1 if malformed.
fn parse_sq(s: &str) -> i32 {
    let b = s.as_bytes();
    if b.len() != 2 || b[0] < b'a' || b[0] > b'h' || b[1] < b'1' || b[1] > b'8' {
        return -1;
    }
    (b[1] - b'1') as i32 * 8 + (b[0] - b'a') as i32
}

/// Render move `m` in long algebraic notation: [piece]from[-|x]to[=Q][+|#].
fn lan(p: &Pos, m: Move) -> String {
    let mut sb = String::new();
    let pc = abs8(p.sq[m.src as usize]);
    if pc != 1 {
        sb.push(PCS[pc as usize] as char);
    }
    sb.push_str(&sq_name(m.src));
    if p.sq[m.dst as usize] != 0 {
        sb.push('x');
    } else {
        sb.push('-');
    }
    sb.push_str(&sq_name(m.dst));
    if m.pro != 0 {
        sb.push('=');
        sb.push(PCS[m.pro as usize] as char);
    }
    let nx = apply(p, m);
    if attacked(&nx, king_sq(&nx, nx.wtm), !nx.wtm) {
        if legal_moves(&nx).is_empty() {
            sb.push('#');
        } else {
            sb.push('+');
        }
    }
    sb
}

/// Extract src, dst and promotion from a LAN token, ignoring decorative piece
/// letters, capture and check marks. Returns None for non-move tokens such as
/// move numbers ("12.") and results ("*").
fn parse_lan(s: &str) -> Option<Move> {
    let mut s = s.trim_end_matches(['+', '#']).to_string();
    let mut pro: i8 = 0;
    if let Some(i) = s.find('=') {
        if i + 1 < s.len() {
            let ch = s.as_bytes()[i + 1];
            pro = PCS.iter().position(|&x| x == ch).map(|p| p as i8).unwrap_or(-1);
            s = s[..i].to_string();
        }
    }
    if !s.is_empty() && b"NBRQK".contains(&s.as_bytes()[0]) {
        s = s[1..].to_string(); // drop a leading piece letter
    }
    let s = s.replace(['-', 'x'], "");
    if s.len() != 4 || pro < 0 {
        return None;
    }
    let a = parse_sq(&s[..2]);
    let b = parse_sq(&s[2..]);
    if a < 0 || b < 0 {
        return None;
    }
    Some(Move { src: a, dst: b, pro })
}

// --- bit streams ----------------------------------------------------------

/// Yields bits MSB-first from `dat`, returning zeros once exhausted.
struct BitReader {
    dat: Vec<u8>,
    pos: usize,
}

impl BitReader {
    /// Consume the next `k` bits and return them as an integer in [0, 2^k).
    fn read(&mut self, k: i32) -> i32 {
        let mut v = 0;
        for _ in 0..k {
            v <<= 1;
            if self.pos < self.dat.len() * 8 {
                v |= ((self.dat[self.pos / 8] >> ((7 - self.pos % 8) as u32)) & 1) as i32;
            }
            self.pos += 1;
        }
        v
    }

    /// Whether every real bit of the input has been consumed.
    fn done(&self) -> bool {
        self.pos >= self.dat.len() * 8
    }
}

/// Packs values MSB-first into a growing byte buffer.
struct BitWriter {
    buf: Vec<u8>,
    cnt: usize,
}

impl BitWriter {
    /// Append the low `k` bits of `v`.
    fn write(&mut self, v: i32, k: i32) {
        let mut n = k - 1;
        while n >= 0 {
            if self.cnt.is_multiple_of(8) {
                self.buf.push(0);
            }
            if (v >> n) & 1 == 1 {
                self.buf[self.cnt / 8] |= 1u8 << ((7 - self.cnt % 8) as u32);
            }
            self.cnt += 1;
            n -= 1;
        }
    }

    /// The first `n` bytes written, zero-padded if the buffer is short and
    /// truncated (dropping move-padding bits) if it is long.
    fn take(&self, n: usize) -> Vec<u8> {
        let mut out = vec![0u8; n];
        let cp = n.min(self.buf.len());
        out[..cp].copy_from_slice(&self.buf[..cp]);
        out
    }
}

/// floor(log2 n): the number of choice bits a position with `n` legal moves can
/// carry (0 when there is only one move, so no real choice).
fn bits(n: i32) -> i32 {
    let mut k = 0;
    while 1 << (k + 1) <= n {
        k += 1;
    }
    k
}

// --- encode / decode ------------------------------------------------------

/// Turn arbitrary bytes into PGN. Walk the game tree, at each position reading
/// floor(log2 N) bits and playing the move with that index; when a game ends
/// (mate/stalemate or the ply cap) with bits still to spend, open a fresh game
/// block. The byte count rides in game 1's [Bytes] tag.
fn encode(dat: &[u8]) -> String {
    let mut rd = BitReader { dat: dat.to_vec(), pos: 0 };
    let mut sb = String::new();
    let mut gm = 0;
    while !rd.done() {
        gm += 1;
        let mut p = start_pos();
        let mut txt: Vec<String> = Vec::new();
        for _ in 0..PLY_CAP {
            if rd.done() {
                break;
            }
            let lm = legal_moves(&p);
            if lm.is_empty() {
                break; // checkmate or stalemate: this game is over
            }
            let mut idx = 0;
            let k = bits(lm.len() as i32);
            if k > 0 {
                idx = rd.read(k);
            }
            txt.push(lan(&p, lm[idx as usize]));
            p = apply(&p, lm[idx as usize]);
        }
        game(&mut sb, gm, dat.len(), &txt);
    }
    if gm == 0 {
        game(&mut sb, 1, 0, &[]); // empty input still yields one valid, empty game
    }
    sb
}

/// Write one PGN game block: a tag header (with [Bytes] on the first game)
/// followed by numbered movetext ending in the "*" unfinished-game marker.
fn game(sb: &mut String, gm: i32, n: usize, txt: &[String]) {
    sb.push_str(&format!("[Event \"gambit\"]\n[Round \"{}\"]\n", gm));
    if gm == 1 {
        sb.push_str(&format!("[Bytes \"{}\"]\n", n));
    }
    sb.push('\n');
    for (i, s) in txt.iter().enumerate() {
        if i % 2 == 0 {
            sb.push_str(&format!("{}. ", i / 2 + 1));
        }
        sb.push_str(s);
        sb.push(' ');
    }
    sb.push_str("*\n\n");
}

/// Reverse `encode`: replay every move of every game, recover each move's index
/// and write those bits, then trim to the stored byte count.
fn decode(pgn: &str) -> Result<Vec<u8>, String> {
    let n = read_n(pgn)?;
    let mut wr = BitWriter { buf: Vec::new(), cnt: 0 };
    for gt in games(pgn) {
        let mut p = start_pos();
        for tok in text(&gt).split_whitespace() {
            let m = match parse_lan(tok) {
                Some(m) => m,
                None => continue, // move number, result, or stray tag fragment
            };
            let lm = legal_moves(&p);
            let ix = find(&lm, m);
            if ix < 0 {
                return Err(format!("illegal move {:?} for this position", tok));
            }
            wr.write(ix, bits(lm.len() as i32));
            p = apply(&p, m);
        }
    }
    // A real game always encodes at least n bytes and n is never negative, so a
    // [Bytes] count outside [0, decoded] is a malformed (or hostile) tag.
    if n < 0 || n as usize > wr.buf.len() {
        return Err(format!("invalid byte count {} for the {} bytes the games encode", n, wr.buf.len()));
    }
    Ok(wr.take(n as usize))
}

/// Read the byte count from the [Bytes "N"] tag.
fn read_n(pgn: &str) -> Result<i64, String> {
    let pat = "[Bytes \"";
    let i = match pgn.find(pat) {
        Some(i) => i + pat.len(),
        None => return Err("not a gambit PGN: no [Bytes] tag".to_string()),
    };
    let j = match pgn[i..].find('"') {
        Some(j) => j,
        None => return Err("malformed [Bytes] tag".to_string()),
    };
    pgn[i..i + j].parse::<i64>().map_err(|_| "malformed byte count".to_string())
}

/// Split multi-game PGN text into one string per game.
fn games(pgn: &str) -> Vec<String> {
    let mut gs: Vec<String> = Vec::new();
    for g in pgn.split("[Event") {
        if !g.trim().is_empty() {
            gs.push(g.to_string());
        }
    }
    gs
}

/// Drop bracketed tag lines from a game, leaving only its movetext.
fn text(g: &str) -> String {
    let mut sb = String::new();
    for ln in g.split('\n') {
        if !ln.trim_start().starts_with('[') {
            sb.push_str(ln);
            sb.push(' ');
        }
    }
    sb
}

// --- cli helpers ----------------------------------------------------------

/// Report an error to stderr and exit non-zero.
fn die(msg: &str) -> ! {
    eprintln!("gambit: {}", msg);
    std::process::exit(1);
}

/// Read the named file, or stdin when `pth` is empty.
fn read_in(pth: &str) -> Vec<u8> {
    if pth.is_empty() {
        let mut buf = Vec::new();
        match std::io::stdin().read_to_end(&mut buf) {
            Ok(_) => buf,
            Err(e) => die(&e.to_string()),
        }
    } else {
        match std::fs::read(pth) {
            Ok(b) => b,
            Err(e) => die(&e.to_string()),
        }
    }
}

/// Write `b` to the named file, or stdout when `pth` is empty.
fn write_out(pth: &str, b: &[u8]) {
    if pth.is_empty() {
        if std::io::stdout().write_all(b).is_err() {
            die("cannot write to stdout");
        }
        return;
    }
    if let Err(e) = std::fs::write(pth, b) {
        die(&e.to_string());
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    // The headline guarantee, mirrored from the Go suite: decode(encode(x)) == x
    // across sizes including the empty slice, byte-unaligned lengths, and inputs
    // long enough to spill across several game blocks.
    #[test]
    fn round_trip() {
        for sz in [0usize, 1, 2, 3, 7, 8, 9, 31, 64, 200, 500, 1000] {
            let mut inp = vec![0u8; sz];
            for i in 0..sz {
                inp[i] = (i.wrapping_mul(2654435761) >> 5) as u8; // deterministic spread (usize math)
            }
            let out = decode(&encode(&inp)).expect("decode");
            assert_eq!(inp, out, "round trip mismatch at size {}", sz);
        }
    }

    #[test]
    fn all_byte_values() {
        let inp: Vec<u8> = (0..=255u16).map(|v| v as u8).collect();
        assert_eq!(inp, decode(&encode(&inp)).unwrap());
    }

    #[test]
    fn start_has_20_moves() {
        assert_eq!(legal_moves(&start_pos()).len(), 20);
    }

    #[test]
    fn bits_is_floor_log2() {
        for (n, ex) in [(1, 0), (2, 1), (3, 1), (4, 2), (7, 2), (8, 3), (20, 4)] {
            assert_eq!(bits(n), ex);
        }
    }

    #[test]
    fn multi_game_for_big_input() {
        let inp = vec![0xABu8; 1000];
        assert!(encode(&inp).matches("[Event").count() >= 2);
        assert_eq!(inp, decode(&encode(&inp)).unwrap());
    }

    // Malformed byte counts must be rejected, not crash (the bugs the Go fuzzer
    // found: a huge count and a negative count).
    #[test]
    fn rejects_bad_byte_count() {
        assert!(decode("[Bytes \"99999999999\"]\n\n*").is_err());
        assert!(decode("[Bytes \"-1\"]\n\n*").is_err());
        assert!(decode("1. e2-e4 *").is_err()); // no [Bytes] tag
    }
}
