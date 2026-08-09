package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestRunWritesRequiredAssetIntegrityFields(t *testing.T) {
	dir := t.TempDir()
	assetPath := filepath.Join(dir, "ao3-hub-linux-x64")
	assetBody := []byte("release binary")
	if err := os.WriteFile(assetPath, assetBody, 0o755); err != nil {
		t.Fatal(err)
	}
	outPath := filepath.Join(dir, "manifest.json")
	if err := run(options{
		repo:        "owner/repo",
		tag:         "v1.2.3",
		out:         outPath,
		baseURL:     "https://github.example",
		channel:     "stable",
		version:     "1.2.3",
		notes:       "test",
		publishedAt: "2026-08-10T00:00:00Z",
		files:       []string{assetPath},
	}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	var got manifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Assets) != 1 {
		t.Fatalf("assets = %d", len(got.Assets))
	}
	digest := sha256.Sum256(assetBody)
	asset := got.Assets[0]
	if asset.SHA256 != hex.EncodeToString(digest[:]) || asset.Size != int64(len(assetBody)) {
		t.Fatalf("asset integrity fields = %+v", asset)
	}
	if asset.URL != "https://github.example/owner/repo/releases/download/v1.2.3/ao3-hub-linux-x64" {
		t.Fatalf("asset URL = %q", asset.URL)
	}
}

func TestParseArgsRejectsInsecureBaseURL(t *testing.T) {
	_, err := parseArgs([]string{
		"--repo", "owner/repo",
		"--tag", "v1.2.3",
		"--out", "manifest.json",
		"--base-url", "http://github.example",
		"ao3-hub-linux-x64",
	})
	if err == nil {
		t.Fatal("insecure base URL was accepted")
	}
}
