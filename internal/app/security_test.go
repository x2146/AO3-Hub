package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStoreRejectsStoryPathTraversal(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(root, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	invalidIDs := []string{"..", "../..", "a/b", `a\b`, ".hidden", ""}
	for _, id := range invalidIDs {
		t.Run(id, func(t *testing.T) {
			if err := store.RemoveStory(id); err == nil {
				t.Fatal("expected invalid story id to be rejected")
			}
			if err := store.SaveMeta(id, Meta{ID: id}); err == nil {
				t.Fatal("expected invalid story id write to be rejected")
			}
		})
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("path traversal affected file outside stories: %v", err)
	}
}

func TestEncodedStoryPathTraversalCannotDeleteOutsideStories(t *testing.T) {
	root := t.TempDir()
	store, err := NewStore(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	user, err := store.CreateUser("admin", "unused", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	session, err := store.CreateSession(user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	app := &App{
		store:    store,
		bus:      NewEventBus(),
		ctx:      context.Background(),
		inflight: map[string]map[string]bool{},
	}
	sentinel := filepath.Join(root, "sentinel")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodDelete, "/api/stories/%2e%2e%2f%2e%2e", nil)
	request.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	if response.Code >= 200 && response.Code < 300 {
		t.Fatalf("traversal status = %d, expected rejection", response.Code)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("encoded path traversal removed file outside stories: %v", err)
	}
}

func TestCorruptUserStoreFailsClosed(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	usersPath := filepath.Join(dataDir, "users.json")
	corrupt := []byte(`{"users":[`)
	if err := os.WriteFile(usersPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := store.UserCount(); err == nil {
		t.Fatal("expected corrupt users file to fail")
	}
	if _, err := store.CreateInitialAdmin("attacker", "hash"); err == nil {
		t.Fatal("corrupt users file reopened initial setup")
	}
	got, err := os.ReadFile(usersPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, corrupt) {
		t.Fatalf("corrupt users file was overwritten: %q", got)
	}
}

func TestCorruptConfigIsNotOverwritten(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(dataDir, "config.json")
	corrupt := []byte(`{"llm":`)
	if err := os.WriteFile(configPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadConfig(); err == nil {
		t.Fatal("expected corrupt config to fail")
	}
	got, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, corrupt) {
		t.Fatalf("corrupt config was overwritten: %q", got)
	}
}

func TestCreateInitialAdminIsAtomic(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const attempts = 16
	start := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	succeeded := 0
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, err := store.CreateInitialAdmin(randomStoryID(), "hash"); err == nil {
				mu.Lock()
				succeeded++
				mu.Unlock()
			}
		}()
	}
	close(start)
	wg.Wait()
	if succeeded != 1 {
		t.Fatalf("successful initial admins = %d, want 1", succeeded)
	}
	count, err := store.UserCount()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("user count = %d, want 1", count)
	}
}

func TestConcurrentAdminDemotionPreservesAnAdmin(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	first, err := store.CreateUser("first", "hash", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	second, err := store.CreateUser("second", "hash", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for _, pair := range [][2]string{{first.ID, second.ID}, {second.ID, first.ID}} {
		pair := pair
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = store.UpdateUserAsAdmin(pair[0], pair[1], "", RoleUser)
		}()
	}
	close(start)
	wg.Wait()

	users, err := store.ListPublicUsers()
	if err != nil {
		t.Fatal(err)
	}
	admins := 0
	for _, user := range users {
		if user.Role == RoleAdmin {
			admins++
		}
	}
	if admins != 1 {
		t.Fatalf("admin count = %d, want 1", admins)
	}
}

func TestAdminCannotChangeOwnRole(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	admin, err := store.CreateUser("admin", "hash", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.UpdateUserAsAdmin(admin.ID, admin.ID, "", RoleUser); !errors.Is(err, errCannotModifySelf) {
		t.Fatalf("self role change error = %v", err)
	}
}

func TestMergeConfigNeverPersistsSecretMasks(t *testing.T) {
	cfg := defaultConfig()
	cfg.LLM.APIKey = "shortkey"
	cfg.AO3.Cookie = "session=value"
	raw := map[string]json.RawMessage{
		"llm": json.RawMessage(`{"apiKey":"********","concurrency":4}`),
		"ao3": json.RawMessage(`{"cookie":"***","userAgent":"test-agent"}`),
	}
	mergeConfig(&cfg, raw)
	if cfg.LLM.APIKey != "shortkey" {
		t.Fatalf("API key = %q, want original short key", cfg.LLM.APIKey)
	}
	if cfg.AO3.Cookie != "session=value" {
		t.Fatalf("AO3 cookie = %q, want original cookie", cfg.AO3.Cookie)
	}
	if cfg.LLM.Concurrency != 4 || cfg.AO3.UserAgent != "test-agent" {
		t.Fatal("non-secret config fields were not updated")
	}
	raw = map[string]json.RawMessage{
		"llm": json.RawMessage(`{"apiKey":""}`),
		"ao3": json.RawMessage(`{"cookie":""}`),
	}
	mergeConfig(&cfg, raw)
	if cfg.LLM.APIKey != "" || cfg.AO3.Cookie != "" {
		t.Fatal("explicitly cleared secrets were unexpectedly retained")
	}
}

func TestAnonymousTranslationStatusOmitsRawLLMData(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const storyID = "12345"
	if err := store.SaveStatsSample(storyID, RequestSample{
		Stage:           StageTranslateBatch,
		CapturedAt:      nowISO(),
		SystemPrompt:    "secret system prompt",
		UserPayload:     "private story payload",
		ResponsePreview: "provider response",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.AppendStatsEvent(storyID, LLMCallEvent{
		ID:           "event-1",
		Stage:        StageTranslateBatch,
		Status:       LLMCallError,
		StartedAt:    nowISO(),
		ErrorMessage: "upstream response body",
	}); err != nil {
		t.Fatal(err)
	}
	app := &App{store: store}
	request := httptest.NewRequest(http.MethodGet, "/api/stories/12345/translation-status", nil)
	request.SetPathValue("id", storyID)
	response := httptest.NewRecorder()
	app.handleTranslationStatus(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d", response.Code)
	}
	var view TranslationStatusView
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if len(view.Events) != 0 || len(view.Samples) != 0 {
		t.Fatalf("anonymous response leaked raw data: events=%d samples=%d", len(view.Events), len(view.Samples))
	}
}

func TestValidateConfigRejectsResourceExhaustionValues(t *testing.T) {
	tests := []func(*Config){
		func(cfg *Config) { cfg.Auth.SessionTTLDays = 1000000 },
		func(cfg *Config) { cfg.Stream.HeartbeatMS = 1 },
		func(cfg *Config) { cfg.LLM.MaxAutoRetries = 10000 },
		func(cfg *Config) { cfg.LLM.MaxTokensPerRequest = 1000000000 },
		func(cfg *Config) { cfg.Update.RestartDelayMS = 1000000000 },
	}
	for i, mutate := range tests {
		cfg := defaultConfig()
		mutate(&cfg)
		if err := validateConfig(cfg); err == nil {
			t.Fatalf("case %d: expected unsafe config to be rejected", i)
		}
	}
}

func TestLoadConfigAcceptsHighConcurrency(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.LLM.Concurrency = 10000
	original, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	original = append(original, '\n')
	configPath := filepath.Join(dataDir, "config.json")
	if err := os.WriteFile(configPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LLM.Concurrency != cfg.LLM.Concurrency {
		t.Fatalf("concurrency = %d, want %d", loaded.LLM.Concurrency, cfg.LLM.Concurrency)
	}
}

func TestCreateSessionEvictsOldestSessionPerUser(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	var tokens []string
	for i := 0; i < maxSessionsPerUser+3; i++ {
		session, err := store.CreateSession("user-1", time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		tokens = append(tokens, session.Token)
	}
	if session, err := store.FindValidSession(tokens[0]); err != nil || session != nil {
		t.Fatalf("oldest session was not evicted: session=%v err=%v", session, err)
	}
	if session, err := store.FindValidSession(tokens[len(tokens)-1]); err != nil || session == nil {
		t.Fatalf("newest session missing: session=%v err=%v", session, err)
	}
	store.mu.Lock()
	file, err := store.loadSessionsLocked()
	store.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Sessions) != maxSessionsPerUser {
		t.Fatalf("stored sessions = %d, want %d", len(file.Sessions), maxSessionsPerUser)
	}
}

func TestLoginGuardBoundsConcurrencyAndFailures(t *testing.T) {
	guard := newLoginAttemptGuard()
	first, err := guard.Begin(context.Background(), "first")
	if err != nil {
		t.Fatal(err)
	}
	second, err := guard.Begin(context.Background(), "second")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := guard.Begin(context.Background(), "third"); !errors.Is(err, errPasswordCheckBusy) {
		t.Fatalf("third concurrent check error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := guard.Begin(canceled, "third"); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled check error = %v", err)
	}
	first(true)
	second(true)

	for i := 0; i < maxFailedLoginAttempts; i++ {
		finish, err := guard.Begin(context.Background(), "same-client")
		if err != nil {
			t.Fatalf("attempt %d unexpectedly blocked: %v", i, err)
		}
		finish(false)
	}
	if _, err := guard.Begin(context.Background(), "same-client"); !errors.Is(err, errLoginRateLimited) {
		t.Fatalf("rate limit error = %v", err)
	}
}

func TestVerifyPasswordRejectsUnsafeArgonParameters(t *testing.T) {
	unsafe := "$argon2id$v=19$m=4294967295,t=2,p=1$c2FsdHNhbHRzYWx0$MDEyMzQ1Njc4OWFiY2RlZg"
	if verifyPassword("password", unsafe) {
		t.Fatal("unsafe Argon2 parameters were accepted")
	}
}

func TestStoreUsesPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX mode bits are not enforced on Windows")
	}
	dataDir := filepath.Join(t.TempDir(), "data")
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	assertMode := func(path string, want os.FileMode) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s mode = %o, want %o", path, got, want)
		}
	}
	assertMode(dataDir, 0o700)
	assertMode(filepath.Join(dataDir, "config.json"), 0o600)
}

func TestSaveSourceIsAtomicOnWriteFailure(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const storyID = "story"
	if err := store.SaveSource(storyID, "old source"); err != nil {
		t.Fatal(err)
	}
	sourcePath, err := store.storyPath(storyID, "source.html")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(sourcePath+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSource(storyID, "new source"); err == nil {
		t.Fatal("expected source write failure")
	}
	got, ok, err := store.LoadSource(storyID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got != "old source" {
		t.Fatalf("source after failed write = %q, %v", got, ok)
	}
}

func TestCORSRejectsUntrustedOrigins(t *testing.T) {
	handler := (&App{serverHost: "ao3hub.example"}).cors(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	request := httptest.NewRequest(http.MethodPost, "http://ao3hub.example/api/config", nil)
	request.Host = "ao3hub.example"
	request.Header.Set("Origin", "https://attacker.example")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusForbidden)
	}
	if got := response.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected allowed origin %q", got)
	}

	request = httptest.NewRequest(http.MethodPost, "http://ao3hub.example/api/config", nil)
	request.Host = "ao3hub.example"
	request.Header.Set("Origin", "http://ao3hub.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("same-host status = %d, want %d", response.Code, http.StatusNoContent)
	}

	request = httptest.NewRequest(http.MethodPost, "http://attacker.example/api/auth/setup", nil)
	request.Host = "attacker.example"
	request.Header.Set("Origin", "http://attacker.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("rebound host status = %d, want %d", response.Code, http.StatusForbidden)
	}

	request = httptest.NewRequest(http.MethodGet, "http://attacker.example/api/health", nil)
	request.Host = "attacker.example"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("rebound host without origin status = %d, want %d", response.Code, http.StatusForbidden)
	}

	request = httptest.NewRequest(http.MethodGet, "http://ao3hub.example/api/health", nil)
	request.Host = "ao3hub.example"
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("trusted host without origin status = %d, want %d", response.Code, http.StatusNoContent)
	}

	request = httptest.NewRequest(http.MethodPost, "http://ao3hub.example/api/config", nil)
	request.Host = "ao3hub.example"
	request.Header.Set("Origin", "https://ao3hub.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-scheme status = %d, want %d", response.Code, http.StatusForbidden)
	}

	request = httptest.NewRequest(http.MethodPost, "http://ao3hub.example/api/config", nil)
	request.Host = "ao3hub.example"
	request.Header.Set("Origin", "http://ao3hub.example/path")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("origin with path status = %d, want %d", response.Code, http.StatusForbidden)
	}

	wildcardHandler := (&App{serverHost: "0.0.0.0"}).cors(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request = httptest.NewRequest(http.MethodPost, "http://192.0.2.10:3000/api/config", nil)
	request.Host = "192.0.2.10:3000"
	request.Header.Set("Origin", "http://192.0.2.10:3000")
	response = httptest.NewRecorder()
	wildcardHandler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("wildcard literal-IP status = %d, want %d", response.Code, http.StatusNoContent)
	}

	request = httptest.NewRequest(http.MethodPost, "http://attacker.example:3000/api/config", nil)
	request.Host = "attacker.example:3000"
	request.Header.Set("Origin", "http://attacker.example:3000")
	response = httptest.NewRecorder()
	wildcardHandler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("wildcard rebound status = %d, want %d", response.Code, http.StatusForbidden)
	}

	proxyApp := &App{
		serverHost:         "127.0.0.1",
		publicOriginScheme: "https",
		publicOriginHost:   "ao3hub.example",
	}
	proxyHandler := proxyApp.cors(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request = httptest.NewRequest(http.MethodPost, "http://ao3hub.example/api/config", nil)
	request.Host = "ao3hub.example"
	request.RemoteAddr = "192.0.2.20:43100"
	request.Header.Set("Origin", "https://ao3hub.example")
	request.Header.Set("X-Forwarded-Proto", "https")
	response = httptest.NewRecorder()
	proxyHandler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("configured HTTPS proxy status = %d, want %d", response.Code, http.StatusNoContent)
	}

	request = httptest.NewRequest(http.MethodPost, "http://attacker.example/api/auth/setup", nil)
	request.Host = "attacker.example"
	request.RemoteAddr = "127.0.0.1:43100"
	request.Header.Set("Origin", "http://attacker.example")
	request.Header.Set("X-Forwarded-Proto", "http")
	response = httptest.NewRecorder()
	proxyHandler.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("loopback forwarded-header spoof status = %d, want %d", response.Code, http.StatusForbidden)
	}
}

func TestCORSRejectsMalformedOriginWithoutPanic(t *testing.T) {
	handler := (&App{serverHost: "ao3hub.example"}).cors(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, origin := range []string{
		"http://ao3hub example",
		"http://ao3hub\x7f.example",
		"http://[::1",
		"%zz://ao3hub.example",
	} {
		request := httptest.NewRequest(http.MethodPost, "http://ao3hub.example/api/config", nil)
		request.Host = "ao3hub.example"
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusForbidden {
			t.Fatalf("malformed origin %q status = %d, want %d", origin, response.Code, http.StatusForbidden)
		}
	}
}

func TestSecureRequestOnlyTrustsConfiguredPublicOrigin(t *testing.T) {
	app := &App{
		publicOriginScheme: "https",
		publicOriginHost:   "ao3hub.example",
	}
	request := httptest.NewRequest(http.MethodGet, "http://ao3hub.example/", nil)
	request.Header.Set("X-Forwarded-Proto", "https")
	if got := app.effectiveRequestScheme(request); got != "https" {
		t.Fatalf("configured public scheme = %q, want https", got)
	}

	request.Host = "attacker.example"
	request.RemoteAddr = "127.0.0.1:1234"
	if got := app.effectiveRequestScheme(request); got != "http" {
		t.Fatalf("spoofed public scheme = %q, want http", got)
	}

	request.Host = "ao3hub.example"
	if got := (&App{}).effectiveRequestScheme(request); got != "http" {
		t.Fatalf("unconfigured forwarded scheme = %q, want http", got)
	}
}

func TestPublicOriginValidation(t *testing.T) {
	scheme, host, err := parsePublicOrigin("https://ao3hub.example:8443")
	if err != nil {
		t.Fatal(err)
	}
	if scheme != "https" || host != "ao3hub.example:8443" {
		t.Fatalf("configured origin = %s://%s", scheme, host)
	}

	for _, raw := range []string{
		"ao3hub.example",
		"ftp://ao3hub.example",
		"https://user@ao3hub.example",
		"https://ao3hub.example/path",
		"https://ao3hub.example?",
		"https://ao3hub example",
		"https://[::1",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, _, err := parsePublicOrigin(raw); err == nil {
				t.Fatalf("accepted invalid public origin %q", raw)
			}
			cfg := defaultConfig()
			cfg.Server.PublicOrigin = raw
			if err := validateConfig(cfg); err == nil {
				t.Fatalf("validateConfig accepted invalid public origin %q", raw)
			}
		})
	}
}

func TestLoadConfigDoesNotRewriteStableConfig(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	configPath := store.path("config.json")
	wantModTime := time.Unix(123, 0)
	if err := os.Chtimes(configPath, wantModTime, wantModTime); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadConfig(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(wantModTime) {
		t.Fatalf("stable config was rewritten: mtime = %s", info.ModTime())
	}
}

func TestLoadConfigPropagatesWriteFailure(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.LLM.Mode = ""
	configPath := store.path("config.json")
	if err := store.writeJSON(configPath, cfg); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(configPath+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadConfig(); err == nil || !strings.Contains(err.Error(), "write config.json") {
		t.Fatalf("write error = %v", err)
	}
}

func TestSecurityHeadersCoverDocumentsAndAPIResponses(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "http://ao3hub.example/api/health", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	for _, name := range []string{
		"Content-Security-Policy",
		"Cross-Origin-Opener-Policy",
		"Cross-Origin-Resource-Policy",
		"Permissions-Policy",
		"Referrer-Policy",
		"X-Content-Type-Options",
		"X-Frame-Options",
	} {
		if value := response.Header().Get(name); value == "" {
			t.Fatalf("missing %s", name)
		}
	}
	if csp := response.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "frame-ancestors 'none'") || !strings.Contains(csp, "object-src 'none'") {
		t.Fatalf("incomplete CSP: %q", csp)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("API cache control = %q, want no-store", got)
	}
}

func TestRetryEndpointReturnsNotFoundForMissingStory(t *testing.T) {
	app := newLifecycleTestApp(t)
	user, err := app.store.CreateUser("admin", "unused", RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	session, err := app.store.CreateSession(user.ID, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/stories/missing/retry", strings.NewReader(`{}`))
	request.Header.Set("Content-Type", "application/json")
	request.AddCookie(&http.Cookie{Name: cookieName, Value: session.Token})
	response := httptest.NewRecorder()
	app.routes().ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("retry status = %d, want %d: %s", response.Code, http.StatusNotFound, response.Body.String())
	}
}

func TestTranslationStatusPropagatesCorruptContext(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	const storyID = "story"
	if err := store.SaveStatsSample(storyID, RequestSample{Stage: StageTranslateBatch}); err != nil {
		t.Fatal(err)
	}
	contextPath, err := store.storyPath(storyID, "context.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(contextPath, []byte(`{"broken":`), 0o600); err != nil {
		t.Fatal(err)
	}
	app := &App{store: store}
	request := httptest.NewRequest(http.MethodGet, "/api/stories/story/translation-status", nil)
	request.SetPathValue("id", storyID)
	response := httptest.NewRecorder()
	app.handleTranslationStatus(response, request)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusInternalServerError)
	}
}

func TestRunContextStopsCleanlyWhenCanceled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	app := newLifecycleTestApp(t)
	cfg, err := app.store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = port
	if err := app.store.SaveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		done <- app.RunContext(ctx)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunContext did not stop after cancellation")
	}
}

func TestDecodeJSONRejectsOversizeAndTrailingValues(t *testing.T) {
	var body map[string]any
	request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{} {}`))
	if err := decodeJSON(request, &body); err == nil {
		t.Fatal("expected trailing JSON value to be rejected")
	}

	request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(bytes.Repeat([]byte(" "), maxJSONBodyBytes+1)))
	if err := decodeJSON(request, &body); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected oversized body error, got %v", err)
	}
}

func TestReadUploadHTMLBoundsMultipartParts(t *testing.T) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "work.html")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("<html></html>")); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < maxUploadParts; i++ {
		if err := writer.WriteField("extra", "x"); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if _, _, err := readUploadHTML(request); err == nil || !strings.Contains(err.Error(), "too many") {
		t.Fatalf("multipart error = %v", err)
	}
}
