/*
Package chess implements a chess game engine that manages move generation,
position analysis, and game state validation.
The engine uses bitboard operations and lookup tables for efficient move
generation and position analysis. Move generation includes standard piece
moves, captures, castling, en passant, and pawn promotions.
Example usage:

	// Create a position
	pos := NewPosition()

	// Calculate legal moves for current position
	eng := engine{}
	moves := eng.CalcMoves(pos, false)

	// Check game status
	status := eng.Status(pos)
	if status == Checkmate {
		fmt.Println("Game Over - Checkmate")
	}
*/
package chess

import "sync"

// engine implements chess move generation and position analysis.
type engine struct{}

// CalcMoves returns all legal moves for the given position. If first is true,
// returns after finding the first legal move. This is useful for quick position
// validation.
//
// The moves are generated in the following order:
//  1. Standard piece moves and captures
//  2. Castling moves (if available)
//
// Each move is validated to ensure it doesn't leave the king in check
func (engine) CalcMoves(pos *Position, first bool) []Move {
	// generate possible moves
	moves := standardMoves(pos, first)
	// return moves including castles
	return append(moves, castleMoves(pos)...)
}

// Status returns the current game status (Checkmate, Stalemate, or NoMethod)
// based on the position.
//
// The status is determined by:
//   - Whether the side to move is in check
//   - Whether any legal moves exist
//
// If the position has cached valid moves in pos.validMoves, those will be
// used. Otherwise, moves will be calculated to determine the status.
func (engine) Status(pos *Position) Method {
	var hasMove bool
	if pos.validMoves != nil {
		hasMove = len(pos.validMoves) > 0
	} else {
		hasMove = len(engine{}.CalcMoves(pos, true)) > 0
	}
	if !pos.inCheck && !hasMove {
		return Stalemate
	} else if pos.inCheck && !hasMove {
		return Checkmate
	}
	return NoMethod
}

// TODO: don't use globals
//
//nolint:gochecknoglobals // this is a lookup table
var promoPieceTypes = []PieceType{Queen, Rook, Bishop, Knight}

const maxPossibleMoves = 218 // Maximum possible moves in any chess position

// movePool is a pool of Move arrays to reduce allocations
// in the standardMoves function.
//
//nolint:gochecknoglobals // this is a sync pool
var movePool = sync.Pool{
	New: func() interface{} {
		return &[maxPossibleMoves]Move{}
	},
}

// standardMoves generates all standard (non-castling) legal moves for the
// current position. If first is true, returns after finding the first
// legal move.
//
// The function uses a sync.Pool of move arrays to reduce allocations. Each
// move is validated to ensure it doesn't leave the king in check.
func standardMoves(pos *Position, first bool) []Move {
	moves, _ := movePool.Get().(*[maxPossibleMoves]Move)
	defer movePool.Put(moves)
	count := 0

	// Reuse a single Move struct for temporary operations
	var m Move

	bbAllowed := ^pos.board.whiteSqs
	if pos.Turn() == Black {
		bbAllowed = ^pos.board.blackSqs
	}

	for _, p := range allPieces {
		if pos.Turn() != p.Color() {
			continue
		}
		s1BB := pos.board.bbForPiece(p)
		if s1BB == 0 {
			continue
		}
		for s1 := range numOfSquaresInBoard {
			if s1BB&bbForSquare(Square(s1)) == 0 {
				continue
			}
			s2BB := bbForPossibleMoves(pos, p.Type(), Square(s1)) & bbAllowed
			if s2BB == 0 {
				continue
			}
			for s2 := range numOfSquaresInBoard {
				if s2BB&bbForSquare(Square(s2)) == 0 {
					continue
				}

				// Reuse move struct by setting fields directly
				m.s1 = Square(s1)
				m.s2 = Square(s2)
				m.tags = 0 // Reset tags

				if (p == WhitePawn && Square(s2).Rank() == Rank8) || (p == BlackPawn && Square(s2).Rank() == Rank1) {
					for _, pt := range promoPieceTypes {
						m.promo = pt
						addTags(&m, pos)
						if !m.HasTag(inCheck) {
							// Copy the valid move to the array
							moves[count] = m
							count++
							if first {
								// For single move, return fixed array of size 1
								var result [1]Move
								result[0] = moves[0]
								return result[:]
							}
						}
					}
				} else {
					m.promo = 0
					addTags(&m, pos)
					if !m.HasTag(inCheck) {
						moves[count] = m
						count++
						if first {
							var result [1]Move
							result[0] = moves[0]
							return result[:]
						}
					}
				}
			}
		}
	}

	// Need to copy since we're returning array to pool
	result := make([]Move, count)
	copy(result, moves[:count])
	return result
}

// addTags updates a move's tags based on the resulting position.
// Tags include:
//   - Capture: The move captures an opponent's piece
//   - EnPassant: The move is an en passant capture
//   - Check: The move puts the opponent in check
//   - inCheck: The move leaves the moving side's king in check (illegal)
//   - KingSideCastle: The move is a king-side castle
//   - QueenSideCastle: The move is a queen-side castle
func addTags(m *Move, pos *Position) {
	p := pos.board.Piece(m.s1)
	if pos.board.isOccupied(m.s2) {
		m.AddTag(Capture)
	} else if m.s2 == pos.enPassantSquare && p.Type() == Pawn {
		m.AddTag(EnPassant)
	}
	// determine if move is castle
	if (p == WhiteKing && m.s1 == E1) || (p == BlackKing && m.s1 == E8) {
		switch m.s2 {
		case C1, C8:
			m.AddTag(QueenSideCastle)
		case G1, G8:
			m.AddTag(KingSideCastle)
		}
	}
	// determine if in check after move (makes move invalid)
	cp := pos.copy()
	cp.board.update(m)
	if isInCheck(cp) {
		m.AddTag(inCheck)
	}
	// determine if opponent in check after move
	cp.turn = cp.turn.Other()
	if isInCheck(cp) {
		m.AddTag(Check)
	}
}

// isInCheck returns true if the side to move is in check in the given position.
func isInCheck(pos *Position) bool {
	kingSq := pos.board.whiteKingSq
	if pos.Turn() == Black {
		kingSq = pos.board.blackKingSq
	}
	// king should only be missing in tests / examples
	if kingSq == NoSquare {
		return false
	}
	return squaresAreAttacked(pos, kingSq)
}

// squaresAreAttacked returns true if any of the given squares are attacked
// by the opponent in the given position.
//
// The function checks attacks from:
//   - Sliding pieces (queen, rook, bishop)
//   - Knights
//   - Pawns
//   - King
//
//nolint:mnd // this is a formula to determine if a square is attacked
func squaresAreAttacked(pos *Position, sqs ...Square) bool {
	otherColor := pos.Turn().Other()
	occ := ^pos.board.emptySqs
	for _, sq := range sqs {
		// hot path check to see if attack vector is possible
		s2BB := pos.board.blackSqs
		if pos.Turn() == Black {
			s2BB = pos.board.whiteSqs
		}
		if ((diaAttack(occ, sq)|hvAttack(occ, sq))&s2BB)|(bbKnightMoves[sq]&s2BB) == 0 {
			continue
		}
		// check queen attack vector
		queenBB := pos.board.bbForPiece(NewPiece(Queen, otherColor))
		bb := (diaAttack(occ, sq) | hvAttack(occ, sq)) & queenBB
		if bb != 0 {
			return true
		}
		// check rook attack vector
		rookBB := pos.board.bbForPiece(NewPiece(Rook, otherColor))
		bb = hvAttack(occ, sq) & rookBB
		if bb != 0 {
			return true
		}
		// check bishop attack vector
		bishopBB := pos.board.bbForPiece(NewPiece(Bishop, otherColor))
		bb = diaAttack(occ, sq) & bishopBB
		if bb != 0 {
			return true
		}
		// check knight attack vector
		knightBB := pos.board.bbForPiece(NewPiece(Knight, otherColor))
		bb = bbKnightMoves[sq] & knightBB
		if bb != 0 {
			return true
		}
		// check pawn attack vector
		if pos.Turn() == White {
			capRight := (pos.board.bbBlackPawn & ^bbFileH & ^bbRank1) << 7
			capLeft := (pos.board.bbBlackPawn & ^bbFileA & ^bbRank1) << 9
			bb = (capRight | capLeft) & bbForSquare(sq)
			if bb != 0 {
				return true
			}
		} else {
			capRight := (pos.board.bbWhitePawn & ^bbFileH & ^bbRank8) >> 9
			capLeft := (pos.board.bbWhitePawn & ^bbFileA & ^bbRank8) >> 7
			bb = (capRight | capLeft) & bbForSquare(sq)
			if bb != 0 {
				return true
			}
		}
		// check king attack vector
		kingBB := pos.board.bbForPiece(NewPiece(King, otherColor))
		bb = bbKingMoves[sq] & kingBB
		if bb != 0 {
			return true
		}
	}
	return false
}

// bbForPossibleMoves returns a bitboard with 1s in positions where the piece
// of the given type at the given square can potentially move, without considering
// whether the moves would be legal (e.g., leave the king in check).
//
// The function handles movement patterns for:
//   - King: One square in any direction
//   - Queen: Sliding moves in all directions
//   - Rook: Sliding moves horizontally and vertically
//   - Bishop: Sliding moves diagonally
//   - Knight: L-shaped jumps
//   - Pawn: Forward moves and captures, including en passant
func bbForPossibleMoves(pos *Position, pt PieceType, sq Square) bitboard {
	switch pt {
	case King:
		return bbKingMoves[sq]
	case Queen:
		return diaAttack(^pos.board.emptySqs, sq) | hvAttack(^pos.board.emptySqs, sq)
	case Rook:
		return hvAttack(^pos.board.emptySqs, sq)
	case Bishop:
		return diaAttack(^pos.board.emptySqs, sq)
	case Knight:
		return bbKnightMoves[sq]
	case Pawn:
		return pawnMoves(pos, sq)
	}
	return bitboard(0)
}

// castleMoves returns all legal castling moves for the current position.
//
// A castling move is legal if:
//   - The king has castling rights in that direction
//   - The squares between king and rook are empty
//   - The king is not in check
//   - The king does not pass through check
func castleMoves(pos *Position) []Move {
	var moves [2]Move // Maximum of 2 possible castle moves (king side and queen side)
	count := 0

	kingSide := pos.castleRights.CanCastle(pos.Turn(), KingSide)
	queenSide := pos.castleRights.CanCastle(pos.Turn(), QueenSide)

	// white king side
	if pos.turn == White && kingSide &&
		(^pos.board.emptySqs&(bbForSquare(F1)|bbForSquare(G1))) == 0 &&
		!squaresAreAttacked(pos, F1, G1) &&
		!pos.inCheck {
		m := Move{s1: E1, s2: G1}
		m.AddTag(KingSideCastle)
		addTags(&m, pos)
		moves[count] = m
		count++
	}

	// white queen side
	if pos.turn == White && queenSide &&
		(^pos.board.emptySqs&(bbForSquare(B1)|bbForSquare(C1)|bbForSquare(D1))) == 0 &&
		!squaresAreAttacked(pos, C1, D1) &&
		!pos.inCheck {
		m := Move{s1: E1, s2: C1}
		m.AddTag(QueenSideCastle)
		addTags(&m, pos)
		moves[count] = m
		count++
	}

	// black king side
	if pos.turn == Black && kingSide &&
		(^pos.board.emptySqs&(bbForSquare(F8)|bbForSquare(G8))) == 0 &&
		!squaresAreAttacked(pos, F8, G8) &&
		!pos.inCheck {
		m := Move{s1: E8, s2: G8}
		m.AddTag(KingSideCastle)
		addTags(&m, pos)
		moves[count] = m
		count++
	}

	// black queen side
	if pos.turn == Black && queenSide &&
		(^pos.board.emptySqs&(bbForSquare(B8)|bbForSquare(C8)|bbForSquare(D8))) == 0 &&
		!squaresAreAttacked(pos, C8, D8) &&
		!pos.inCheck {
		m := Move{s1: E8, s2: C8}
		m.AddTag(QueenSideCastle)
		addTags(&m, pos)
		moves[count] = m
		count++
	}

	return moves[:count]
}

// pawnMoves returns a bitboard with 1s in positions where the pawn at the
// given square can potentially move.
//
// The function considers:
//   - Single and double forward moves
//   - Diagonal captures
//   - En passant captures
//
//nolint:mnd // this is a formula to determine the color of a square
func pawnMoves(pos *Position, sq Square) bitboard {
	bb := bbForSquare(sq)
	var bbEnPassant bitboard
	if pos.enPassantSquare != NoSquare {
		bbEnPassant = bbForSquare(pos.enPassantSquare)
	}
	if pos.Turn() == White {
		capRight := ((bb & ^bbFileH & ^bbRank8) >> 9) & (pos.board.blackSqs | bbEnPassant)
		capLeft := ((bb & ^bbFileA & ^bbRank8) >> 7) & (pos.board.blackSqs | bbEnPassant)
		upOne := ((bb & ^bbRank8) >> 8) & pos.board.emptySqs
		upTwo := ((upOne & bbRank3) >> 8) & pos.board.emptySqs
		return capRight | capLeft | upOne | upTwo
	}
	capRight := ((bb & ^bbFileH & ^bbRank1) << 7) & (pos.board.whiteSqs | bbEnPassant)
	capLeft := ((bb & ^bbFileA & ^bbRank1) << 9) & (pos.board.whiteSqs | bbEnPassant)
	upOne := ((bb & ^bbRank1) << 8) & pos.board.emptySqs
	upTwo := ((upOne & bbRank6) << 8) & pos.board.emptySqs
	return capRight | capLeft | upOne | upTwo
}

// diaAttack returns a bitboard representing possible diagonal moves for a
// sliding piece, considering occupied squares as blocking further movement.
func diaAttack(occupied bitboard, sq Square) bitboard {
	pos := bbForSquare(sq)
	dMask := bbDiagonals[sq]
	adMask := bbAntiDiagonals[sq]
	return linearAttack(occupied, pos, dMask) | linearAttack(occupied, pos, adMask)
}

// hvAttack returns a bitboard representing possible horizontal and vertical
func hvAttack(occupied bitboard, sq Square) bitboard {
	pos := bbForSquare(sq)
	rankMask := bbRanks[sq.Rank()]
	fileMask := bbFiles[sq.File()]
	return linearAttack(occupied, pos, rankMask) | linearAttack(occupied, pos, fileMask)
}

// linearAttack returns a bitboard representing possible moves in a single
// direction (rank, file, or diagonal) for a sliding piece, considering
// occupied squares as blocking further movement.
func linearAttack(occupied, pos, mask bitboard) bitboard {
	oInMask := occupied & mask
	return ((oInMask - 2*pos) ^ (oInMask.Reverse() - 2*pos.Reverse()).Reverse()) & mask
}

const (
	bbFileA bitboard = 9259542123273814144
	bbFileB bitboard = 4629771061636907072
	bbFileC bitboard = 2314885530818453536
	bbFileD bitboard = 1157442765409226768
	bbFileE bitboard = 578721382704613384
	bbFileF bitboard = 289360691352306692
	bbFileG bitboard = 144680345676153346
	bbFileH bitboard = 72340172838076673

	bbRank1 bitboard = 18374686479671623680
	bbRank2 bitboard = 71776119061217280
	bbRank3 bitboard = 280375465082880
	bbRank4 bitboard = 1095216660480
	bbRank5 bitboard = 4278190080
	bbRank6 bitboard = 16711680
	bbRank7 bitboard = 65280
	bbRank8 bitboard = 255
)

// TODO make method on Square
func bbForSquare(sq Square) bitboard {
	return bbSquares[sq]
}

// Lookup tables for piece movement patterns and board masks.
//
//nolint:gochecknoglobals // this is a lookup table
var (
	bbFiles = [8]bitboard{bbFileA, bbFileB, bbFileC, bbFileD, bbFileE, bbFileF, bbFileG, bbFileH} // bbFiles contains masks for each file (A-H)
	bbRanks = [8]bitboard{bbRank1, bbRank2, bbRank3, bbRank4, bbRank5, bbRank6, bbRank7, bbRank8} // bbRanks contains masks for each rank (1-8)

	bbDiagonals = [64]bitboard{9241421688590303745, 4620710844295151872, 2310355422147575808, 1155177711073755136, 577588855528488960, 288794425616760832, 144396663052566528, 72057594037927936, 36099303471055874, 9241421688590303745, 4620710844295151872, 2310355422147575808, 1155177711073755136, 577588855528488960, 288794425616760832, 144396663052566528, 141012904183812, 36099303471055874, 9241421688590303745, 4620710844295151872, 2310355422147575808, 1155177711073755136, 577588855528488960, 288794425616760832, 550831656968, 141012904183812, 36099303471055874, 9241421688590303745, 4620710844295151872, 2310355422147575808, 1155177711073755136, 577588855528488960, 2151686160, 550831656968, 141012904183812, 36099303471055874, 9241421688590303745, 4620710844295151872, 2310355422147575808, 1155177711073755136, 8405024, 2151686160, 550831656968, 141012904183812, 36099303471055874, 9241421688590303745, 4620710844295151872, 2310355422147575808, 32832, 8405024, 2151686160, 550831656968, 141012904183812, 36099303471055874, 9241421688590303745, 4620710844295151872, 128, 32832, 8405024, 2151686160, 550831656968, 141012904183812, 36099303471055874, 9241421688590303745}

	bbAntiDiagonals = [64]bitboard{9223372036854775808, 4647714815446351872, 2323998145211531264, 1161999622361579520, 580999813328273408, 290499906672525312, 145249953336295424, 72624976668147840, 4647714815446351872, 2323998145211531264, 1161999622361579520, 580999813328273408, 290499906672525312, 145249953336295424, 72624976668147840, 283691315109952, 2323998145211531264, 1161999622361579520, 580999813328273408, 290499906672525312, 145249953336295424, 72624976668147840, 283691315109952, 1108169199648, 1161999622361579520, 580999813328273408, 290499906672525312, 145249953336295424, 72624976668147840, 283691315109952, 1108169199648, 4328785936, 580999813328273408, 290499906672525312, 145249953336295424, 72624976668147840, 283691315109952, 1108169199648, 4328785936, 16909320, 290499906672525312, 145249953336295424, 72624976668147840, 283691315109952, 1108169199648, 4328785936, 16909320, 66052, 145249953336295424, 72624976668147840, 283691315109952, 1108169199648, 4328785936, 16909320, 66052, 258, 72624976668147840, 283691315109952, 1108169199648, 4328785936, 16909320, 66052, 258, 1}

	bbKnightMoves = [64]bitboard{9077567998918656, 4679521487814656, 38368557762871296, 19184278881435648, 9592139440717824, 4796069720358912, 2257297371824128, 1128098930098176, 2305878468463689728, 1152939783987658752, 9799982666336960512, 4899991333168480256, 2449995666584240128, 1224997833292120064, 576469569871282176, 288234782788157440, 4620693356194824192, 11533718717099671552, 5802888705324613632, 2901444352662306816, 1450722176331153408, 725361088165576704, 362539804446949376, 145241105196122112, 18049583422636032, 45053588738670592, 22667534005174272, 11333767002587136, 5666883501293568, 2833441750646784, 1416171111120896, 567348067172352, 70506185244672, 175990581010432, 88545054707712, 44272527353856, 22136263676928, 11068131838464, 5531918402816, 2216203387392, 275414786112, 687463207072, 345879119952, 172939559976, 86469779988, 43234889994, 21609056261, 8657044482, 1075839008, 2685403152, 1351090312, 675545156, 337772578, 168886289, 84410376, 33816580, 4202496, 10489856, 5277696, 2638848, 1319424, 659712, 329728, 132096}

	bbKingMoves = [64]bitboard{4665729213955833856, 11592265440851656704, 5796132720425828352, 2898066360212914176, 1449033180106457088, 724516590053228544, 362258295026614272, 144959613005987840, 13853283560024178688, 16186183351374184448, 8093091675687092224, 4046545837843546112, 2023272918921773056, 1011636459460886528, 505818229730443264, 216739030602088448, 54114388906344448, 63227278716305408, 31613639358152704, 15806819679076352, 7903409839538176, 3951704919769088, 1975852459884544, 846636838289408, 211384331665408, 246981557485568, 123490778742784, 61745389371392, 30872694685696, 15436347342848, 7718173671424, 3307175149568, 825720045568, 964771708928, 482385854464, 241192927232, 120596463616, 60298231808, 30149115904, 12918652928, 3225468928, 3768639488, 1884319744, 942159872, 471079936, 235539968, 117769984, 50463488, 12599488, 14721248, 7360624, 3680312, 1840156, 920078, 460039, 197123, 49216, 57504, 28752, 14376, 7188, 3594, 1797, 770}

	bbSquares = [64]bitboard{}
)

// TODO: remove this init function
//
//nolint:gochecknoinits // will be removed
func init() {
	const numOfSquaresInBoard = 64
	for sq := range numOfSquaresInBoard {
		bbSquares[sq] = bitboard(uint64(1) << (uint8(63) - uint8(sq)))
	}
}
