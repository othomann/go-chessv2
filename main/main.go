package main

import (
	"crypto/rand"
	"fmt"
	"math/big"

	"github.com/othomann/go-chess/v2"
)

func main() {
	game := chess.NewGame()
	// generate moves until game is over
	for game.Outcome() == chess.NoOutcome {
		// select a random move
		moves := game.ValidMoves()
		randomInt, err := rand.Int(rand.Reader, big.NewInt(int64(len(moves))))
		var move chess.Move
		if err == nil {
			move = moves[randomInt.Int64()]
		} else {
			move = moves[0]
		}
		err = game.Move(&move, nil)
		if err != nil {
			fmt.Printf("Wrong move: %s - %s\n", &move, err)
		}
	}
	// print outcome and game PGN
	fmt.Println(game.Position().Board().Draw())
	fmt.Printf("Game completed. %s by %s.\n", game.Outcome(), game.Method())
	fmt.Println(game.String())
}
