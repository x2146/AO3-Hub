package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSigningPrivateKeyAndSignFile(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	t.Setenv(signingKeyEnv, hex.EncodeToString(seed))
	privateKey, err := signingPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "manifest.json")
	message := []byte("{\"version\":\"v1\"}\n")
	if err := os.WriteFile(path, message, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := signFile(privateKey, path); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path + ".sig")
	if err != nil {
		t.Fatal(err)
	}
	signature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(encoded)))
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(privateKey.Public().(ed25519.PublicKey), message, signature) {
		t.Fatal("signature did not cover the exact file bytes")
	}
	if ed25519.Verify(privateKey.Public().(ed25519.PublicKey), append(message, ' '), signature) {
		t.Fatal("signature accepted modified file bytes")
	}
}

func TestSigningPrivateKeyRejectsMissingOrInvalidSeed(t *testing.T) {
	t.Setenv(signingKeyEnv, "")
	if _, err := signingPrivateKey(); err == nil {
		t.Fatal("missing seed was accepted")
	}
	t.Setenv(signingKeyEnv, "00")
	if _, err := signingPrivateKey(); err == nil {
		t.Fatal("short seed was accepted")
	}
}
