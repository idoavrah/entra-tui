package telemetry

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"math/big"
	"os"
	"runtime"
	"sync"
)

// handleWords is the vocabulary terraform-tui draws its handles from, carried
// over so a handle reads the same way in both tools.
//
//go:embed handle_words.json
var handleWords []byte

var (
	wordsOnce  sync.Once
	adjectives []string
	nouns      []string
)

func words() ([]string, []string) {
	wordsOnce.Do(func() {
		var lists struct {
			Adjectives []string `json:"adjectives"`
			Nouns      []string `json:"nouns"`
		}
		if err := json.Unmarshal(handleWords, &lists); err != nil {
			return
		}
		adjectives, nouns = lists.Adjectives, lists.Nouns
	})
	return adjectives, nouns
}

var (
	handleOnce sync.Once
	handle     string
)

// MachineHandle is a stable, non-reversible two-word name for this machine.
//
// It is the whole of what identifies a returning user: no account, no tenant,
// no address. The fingerprint is hashed and then used only to pick two words,
// so the handle cannot be turned back into the machine it came from -- and two
// machines sharing a handle is a collision, not a leak.
func MachineHandle() string {
	handleOnce.Do(func() {
		host, err := os.Hostname()
		if err != nil {
			host = "unknown"
		}
		handle = HandleFor(runtime.GOOS + "-" + host + "-" + runtime.GOARCH)
	})
	return handle
}

// HandleFor derives a two-word handle from an arbitrary fingerprint.
func HandleFor(fingerprint string) string {
	adj, noun := words()
	if len(adj) == 0 || len(noun) == 0 {
		return "anonymous user"
	}
	sum := sha256.Sum256([]byte(fingerprint))
	digest := new(big.Int).SetBytes(sum[:])
	a := new(big.Int).Mod(digest, big.NewInt(int64(len(adj)))).Int64()
	n := new(big.Int).Mod(digest, big.NewInt(int64(len(noun)))).Int64()
	return adj[a] + " " + noun[n]
}
