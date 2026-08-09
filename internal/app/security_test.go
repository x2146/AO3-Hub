package app

import (
	"bytes"
	"context"
	"errors"
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

func TestCORSRejectsUntrustedOrigins(t *testing.T) {
	handler := (&App{}).cors(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
	request.Header.Set("Origin", "https://ao3hub.example")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("same-host status = %d, want %d", response.Code, http.StatusNoContent)
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
