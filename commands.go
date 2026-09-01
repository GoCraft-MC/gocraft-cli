package main

import (
	"fmt"
	"os"

	"github.com/GoCraft-MC/gocraft-abi/command"
)

// The command trees a build tool handed over, turned into what a bundle ships.
//
// §15 splits the work this way on purpose: only a compiler can see a plugin's
// source, and only this writes the bundle format. Every runtime extracts
// differently — an annotation processor inside javac, a Go plugin asked to
// describe itself — and they all hand over the same neutral file rather than a
// commands.pb, so the wire format has exactly one writer.
//
// Neither half is reimplemented here. Reading that file and encoding the tree
// both live in the contract, which the server reads bundles with: a tree this
// refuses is a tree the server would have refused.
func readCommandTree(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read command trees: %w", err)
	}
	root, err := command.DecodeIntermediate(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	// Encoded through the host's own encoder, which validates first, so the
	// failure lands on the machine that has the source.
	return command.EncodeTree(root)
}
