package app

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckForUpdateStableStrategy(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		remote     string
		hasUpdate  bool
		wantReason string
	}{
		{name: "newer stable", current: "v0.1.0", remote: "v0.1.1", hasUpdate: true, wantReason: "远端 stable 版本更新"},
		{name: "same stable ignores v prefix", current: "0.1.1", remote: "v0.1.1", wantReason: "版本相同"},
		{name: "older stable", current: "v0.2.0", remote: "v0.1.1", wantReason: "当前 stable 版本已是最新"},
		{name: "stable can replace dev build", current: "dev-0012-20260523-abcdef0", remote: "v0.1.1", hasUpdate: true, wantReason: "当前为 dev 构建，允许切换到 stable"},
		{name: "stable rejects dev remote", current: "v0.1.0", remote: "dev-0012-20260523-abcdef0", wantReason: "stable channel 需要远端版本是 semver"},
		{name: "unknown current is not comparable", current: "nightly", remote: "v0.1.1", wantReason: "当前版本不是可比较的 semver"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkForUpdate("stable", tt.current, tt.remote)
			if got.Strategy != "stable-semver" {
				t.Fatalf("strategy = %q, want stable-semver", got.Strategy)
			}
			if got.HasUpdate != tt.hasUpdate {
				t.Fatalf("hasUpdate = %v, want %v", got.HasUpdate, tt.hasUpdate)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestCheckForUpdateDevStrategy(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		remote     string
		hasUpdate  bool
		wantReason string
	}{
		{name: "newer dev run", current: "dev-0007-20260401-aaaaaaa", remote: "dev-0042-20260425-bbbbbbb", hasUpdate: true, wantReason: "远端 dev run number 更新"},
		{name: "same commit sha", current: "dev-0007-20260401-aaaaaaa", remote: "dev-0042-20260425-aaaaaaa", wantReason: "commit SHA 相同"},
		{name: "older dev run", current: "dev-0042-20260425-bbbbbbb", remote: "dev-0007-20260401-aaaaaaa", wantReason: "当前 dev run number 已是最新"},
		{name: "local dev accepts ci dev", current: "dev-local", remote: "dev-0042-20260425-bbbbbbb", hasUpdate: true, wantReason: "本地 dev 版本未注入 CI run，允许升级"},
		{name: "stable does not auto switch to dev", current: "v0.1.1", remote: "dev-0042-20260425-bbbbbbb", wantReason: "当前版本不是 dev CI tag，避免自动切换或回退"},
		{name: "invalid remote dev", current: "dev-0007-20260401-aaaaaaa", remote: "v0.1.1", wantReason: "dev channel 需要远端版本是 dev CI tag"},
		{name: "unknown dev current", current: "dev-preview", remote: "dev-0042-20260425-bbbbbbb", wantReason: "当前版本不是 dev CI tag，避免自动切换或回退"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkForUpdate("dev", tt.current, tt.remote)
			if got.Strategy != "dev-run" {
				t.Fatalf("strategy = %q, want dev-run", got.Strategy)
			}
			if got.HasUpdate != tt.hasUpdate {
				t.Fatalf("hasUpdate = %v, want %v", got.HasUpdate, tt.hasUpdate)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestSameVersionIgnoresStableVPrefix(t *testing.T) {
	if !sameVersion("v0.1.1", "0.1.1") {
		t.Fatal("expected v-prefixed stable versions to match")
	}
	if sameVersion("dev-0012-20260523-abcdef0", "dev-0013-20260523-abcdef0") {
		t.Fatal("expected different dev action versions not to match")
	}
}

type signedUpdateFixture struct {
	asset        []byte
	manifestBody []byte
	signature    []byte
	server       *httptest.Server
}

func newUpdateTestServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	originalClient := updateHTTPClient
	client := server.Client()
	client.CheckRedirect = originalClient.CheckRedirect
	updateHTTPClient = client
	t.Cleanup(func() {
		updateHTTPClient = originalClient
		server.Close()
	})
	return server
}

func newSignedUpdateFixture(t *testing.T, privateKey ed25519.PrivateKey, mutate func(*Manifest)) *signedUpdateFixture {
	t.Helper()
	fixture := &signedUpdateFixture{asset: []byte("verified update binary")}
	fixture.server = newUpdateTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/manifest.json":
			_, _ = w.Write(fixture.manifestBody)
		case "/manifest.json.sig":
			_, _ = w.Write(fixture.signature)
		case "/asset":
			_, _ = w.Write(fixture.asset)
		default:
			http.NotFound(w, r)
		}
	}))
	digest := sha256.Sum256(fixture.asset)
	manifest := Manifest{
		Version: "v9.9.9",
		Channel: "stable",
		Assets: []ManifestAsset{{
			Platform: platformName(),
			Arch:     archName(),
			URL:      fixture.server.URL + "/asset",
			SHA256:   hex.EncodeToString(digest[:]),
			Size:     int64(len(fixture.asset)),
		}},
	}
	if mutate != nil {
		mutate(&manifest)
	}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	fixture.manifestBody = append(body, '\n')
	fixture.signature = []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(privateKey, fixture.manifestBody)) + "\n")
	return fixture
}

func setUpdateTestSigningKey(t *testing.T) ed25519.PrivateKey {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	original := UpdateSigningPublicKey
	UpdateSigningPublicKey = hex.EncodeToString(publicKey)
	t.Cleanup(func() { UpdateSigningPublicKey = original })
	return privateKey
}

func TestFetchManifestVerifiesExactRawBytes(t *testing.T) {
	privateKey := setUpdateTestSigningKey(t)
	fixture := newSignedUpdateFixture(t, privateKey, nil)
	cfg := Config{Update: UpdateConfig{ManifestURL: fixture.server.URL + "/manifest.json"}}

	manifest, message := fetchManifest(cfg)
	if manifest == nil || message != "" {
		t.Fatalf("fetchManifest() manifest=%v message=%q", manifest, message)
	}
	fixture.manifestBody = append(fixture.manifestBody, ' ')
	if manifest, message := fetchManifest(cfg); manifest != nil || !strings.Contains(message, "signature") {
		t.Fatalf("tampered manifest was accepted: manifest=%v message=%q", manifest, message)
	}
}

func TestFetchManifestRejectsMissingKeyBeforeNetwork(t *testing.T) {
	requests := 0
	server := newUpdateTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.NotFound(w, r)
	}))
	originalKey := UpdateSigningPublicKey
	UpdateSigningPublicKey = ""
	t.Cleanup(func() { UpdateSigningPublicKey = originalKey })

	manifest, message := fetchManifest(Config{Update: UpdateConfig{ManifestURL: server.URL + "/manifest.json"}})
	if manifest != nil || !strings.Contains(message, "embedded update signing public key") {
		t.Fatalf("manifest=%v message=%q", manifest, message)
	}
	if requests != 0 {
		t.Fatalf("made %d network requests without a trusted key", requests)
	}
}

func TestApplyUpdateForceCannotBypassSignature(t *testing.T) {
	privateKey := setUpdateTestSigningKey(t)
	fixture := newSignedUpdateFixture(t, privateKey, nil)
	fixture.signature = []byte(base64.StdEncoding.EncodeToString(make([]byte, ed25519.SignatureSize)))

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.Update.ManifestURL = fixture.server.URL + "/manifest.json"
	if err := store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	result := (&App{store: store}).ApplyUpdate(ApplyUpdateOptions{Force: true})
	if result.OK || !strings.Contains(result.Message, "signature") {
		t.Fatalf("forced update result = %+v", result)
	}
}

func TestFetchManifestRequiresAssetIntegrityFields(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Manifest)
		want   string
	}{
		{name: "missing sha256", mutate: func(m *Manifest) { m.Assets[0].SHA256 = "" }, want: "sha256"},
		{name: "missing size", mutate: func(m *Manifest) { m.Assets[0].Size = 0 }, want: "size"},
		{name: "public http", mutate: func(m *Manifest) { m.Assets[0].URL = "http://example.com/update" }, want: "HTTPS"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			privateKey := setUpdateTestSigningKey(t)
			fixture := newSignedUpdateFixture(t, privateKey, test.mutate)
			cfg := Config{Update: UpdateConfig{ManifestURL: fixture.server.URL + "/manifest.json"}}
			manifest, message := fetchManifest(cfg)
			if manifest != nil || !strings.Contains(message, test.want) {
				t.Fatalf("manifest=%v message=%q, want %q", manifest, message, test.want)
			}
		})
	}
}

func TestUpdateRedirectRejectsPublicHTTP(t *testing.T) {
	server := newUpdateTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://192.0.2.1/manifest.json", http.StatusFound)
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, err := fetchUpdateBytes(ctx, server.URL, 1024)
	if err == nil || !strings.Contains(err.Error(), "HTTPS") {
		t.Fatalf("redirect error = %v", err)
	}
}

func TestValidateUpdateURLRequiresHTTPSForLoopback(t *testing.T) {
	for _, rawURL := range []string{"http://localhost/update", "http://127.0.0.1/update", "http://[::1]/update"} {
		if err := validateUpdateURL(rawURL); err == nil || !strings.Contains(err.Error(), "HTTPS") {
			t.Fatalf("validateUpdateURL(%q) error = %v", rawURL, err)
		}
	}
}

func TestDownloadUpdateAssetVerifiesSizeAndHash(t *testing.T) {
	assetBody := []byte("complete binary")
	server := newUpdateTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(assetBody)
	}))
	digest := sha256.Sum256(assetBody)
	asset := ManifestAsset{
		URL:    server.URL,
		SHA256: hex.EncodeToString(digest[:]),
		Size:   int64(len(assetBody)),
	}
	path, err := downloadUpdateAsset(context.Background(), t.TempDir(), "v1.2.3", asset)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(assetBody) {
		t.Fatalf("downloaded body = %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Fatalf("download mode = %o", info.Mode().Perm())
	}
}

func TestDownloadUpdateAssetRejectsChunkedOverrun(t *testing.T) {
	assetBody := []byte("expected")
	server := newUpdateTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		_, _ = w.Write(append(assetBody, '!'))
	}))
	digest := sha256.Sum256(assetBody)
	dir := t.TempDir()
	_, err := downloadUpdateAsset(context.Background(), dir, "v1.2.3", ManifestAsset{
		URL:    server.URL,
		SHA256: hex.EncodeToString(digest[:]),
		Size:   int64(len(assetBody)),
	})
	if err == nil || !strings.Contains(err.Error(), "大小") {
		t.Fatalf("overrun error = %v", err)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("temporary download was not cleaned up: %v", entries)
	}
}

func TestDownloadUpdateAssetHonorsCancellation(t *testing.T) {
	server := newUpdateTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := downloadUpdateAsset(ctx, t.TempDir(), "v1", ManifestAsset{
		URL:    server.URL,
		SHA256: strings.Repeat("0", 64),
		Size:   1,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation error = %v", err)
	}
}

func TestApplyUpdateRejectsConcurrentRun(t *testing.T) {
	app := &App{}
	app.updateMu.Lock()
	defer app.updateMu.Unlock()
	result := app.ApplyUpdate(ApplyUpdateOptions{})
	if result.OK || !strings.Contains(result.Message, "正在进行") {
		t.Fatalf("concurrent result = %+v", result)
	}
}

func TestApplyUpdateRejectsRunWhileRestartPending(t *testing.T) {
	app := &App{updateRestartPending: true}
	result := app.ApplyUpdate(ApplyUpdateOptions{})
	if result.OK || !strings.Contains(result.Message, "等待进程重启") {
		t.Fatalf("restart-pending result = %+v", result)
	}
}

func TestInstallUpdateAtUsesPreparedRenameAndKeepsBackup(t *testing.T) {
	dir := t.TempDir()
	execPath := filepath.Join(dir, "ao3-hub")
	newPath := filepath.Join(dir, "downloaded")
	if err := os.WriteFile(execPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(newPath, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := installUpdateAt(newPath, execPath); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(execPath)
	if err != nil {
		t.Fatal(err)
	}
	backup, err := os.ReadFile(execPath + ".bak")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" || string(backup) != "old" {
		t.Fatalf("installed=%q backup=%q", got, backup)
	}
	matches, err := filepath.Glob(filepath.Join(dir, ".ao3-hub-update-*.tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) != 0 {
		t.Fatalf("prepared files remain: %v", matches)
	}
}

func TestReplacePreparedExecutableReportsRollbackFailure(t *testing.T) {
	calls := 0
	rename := func(_, _ string) error {
		calls++
		switch calls {
		case 1:
			return nil
		case 2:
			return errors.New("install failed")
		default:
			return errors.New("rollback failed")
		}
	}
	execPath := filepath.Join(t.TempDir(), "ao3-hub")
	err := replacePreparedExecutableWithRename("prepared", execPath, rename)
	if err == nil || !strings.Contains(err.Error(), "install failed") || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatalf("combined rollback error = %v", err)
	}
}

func TestDownloadUpdateAssetRejectsShortBody(t *testing.T) {
	server := newUpdateTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "2")
		_, _ = io.WriteString(w, "x")
	}))
	_, err := downloadUpdateAsset(context.Background(), t.TempDir(), "v1", ManifestAsset{
		URL:    server.URL,
		SHA256: strings.Repeat("0", 64),
		Size:   2,
	})
	if err == nil {
		t.Fatal("short response was accepted")
	}
}
