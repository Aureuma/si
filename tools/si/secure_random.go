package main

import (
	"crypto/rand"
	"fmt"
	"math/big"
)

func secureIntn(max int) (int, error) {
	if max <= 0 {
		return 0, fmt.Errorf("max must be > 0")
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(max)))
	if err != nil {
		return 0, err
	}
	return int(n.Int64()), nil
}
