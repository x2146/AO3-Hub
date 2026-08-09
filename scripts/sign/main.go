package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

const signingKeyEnv = "UPDATE_SIGNING_KEY"

func main() {
	genKey := flag.Bool("genkey", false, "generate an Ed25519 signing seed and public key")
	pubKey := flag.Bool("pubkey", false, "derive the public key from UPDATE_SIGNING_KEY")
	flag.Parse()

	if *genKey && *pubKey {
		fatal(errors.New("-genkey and -pubkey are mutually exclusive"))
	}
	if *genKey {
		_, privateKey, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			fatal(err)
		}
		seed := privateKey.Seed()
		fmt.Printf("private seed: %s\n", hex.EncodeToString(seed))
		fmt.Printf("public key: %s\n", hex.EncodeToString(privateKey.Public().(ed25519.PublicKey)))
		return
	}

	privateKey, err := signingPrivateKey()
	if err != nil {
		fatal(err)
	}
	if *pubKey {
		fmt.Println(hex.EncodeToString(privateKey.Public().(ed25519.PublicKey)))
		return
	}
	if flag.NArg() == 0 {
		fatal(errors.New("provide at least one file to sign, or use -genkey/-pubkey"))
	}
	for _, path := range flag.Args() {
		if err := signFile(privateKey, path); err != nil {
			fatal(err)
		}
		fmt.Printf("signed %s\n", path)
	}
}

func signingPrivateKey() (ed25519.PrivateKey, error) {
	raw := strings.TrimSpace(os.Getenv(signingKeyEnv))
	if raw == "" {
		return nil, fmt.Errorf("%s is not set", signingKeyEnv)
	}
	seed, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", signingKeyEnv, err)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, fmt.Errorf("%s must be a %d-byte Ed25519 seed encoded as hex", signingKeyEnv, ed25519.SeedSize)
	}
	return ed25519.NewKeyFromSeed(seed), nil
}

func signFile(privateKey ed25519.PrivateKey, path string) error {
	message, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	signature := ed25519.Sign(privateKey, message)
	encoded := base64.StdEncoding.EncodeToString(signature) + "\n"
	if err := os.WriteFile(path+".sig", []byte(encoded), 0o644); err != nil {
		return fmt.Errorf("write %s.sig: %w", path, err)
	}
	return nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
