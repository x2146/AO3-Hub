package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"ao3hub/internal/webassets"
)

type App struct {
	store                *Store
	bus                  *EventBus
	queue                *Queue
	ctx                  context.Context
	cancel               context.CancelFunc
	closeOnce            sync.Once
	storyMu              sync.Mutex
	storyGates           map[string]*storyGate
	inflightMu           sync.RWMutex
	inflight             map[string]map[string]bool
	loginOnce            sync.Once
	loginGuard           *loginAttemptGuard
	updateMu             sync.Mutex
	updateRestartPending bool
	updateCache          updateManifestCache
}

type storyGate struct {
	token chan struct{}
	refs  int
}

func New() (*App, error) {
	dir, err := dataDir()
	if err != nil {
		return nil, err
	}
	store, err := NewStore(dir)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	app := &App{
		store:    store,
		bus:      NewEventBus(),
		ctx:      ctx,
		cancel:   cancel,
		inflight: map[string]map[string]bool{},
	}
	app.queue = NewQueue(app)
	return app, nil
}

func (a *App) Run() error {
	return a.RunContext(context.Background())
}

func (a *App) RunContext(ctx context.Context) error {
	defer a.Close()
	cfg, err := a.store.LoadConfig()
	if err != nil {
		return err
	}
	if err := a.ResumeOnStartup(); err != nil {
		return err
	}
	a.maybeAutoCheckUpdates(cfg)
	host := resolveHost(cfg.Server.Host)
	port, err := resolvePort(cfg.Server.Port)
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	server := &http.Server{
		Addr:              addr,
		Handler:           a.routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	fmt.Printf("[ao3-hub] %s listening on http://%s\n", versionLabel(Version), listener.Addr())
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()
	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		shutdownErr := make(chan error, 1)
		go func() {
			shutdownErr <- server.Shutdown(shutdownCtx)
		}()
		a.Close()
		if err := <-shutdownErr; err != nil {
			_ = server.Close()
			return fmt.Errorf("shut down HTTP server: %w", err)
		}
		if err := <-serveErr; err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
}

func (a *App) Close() {
	a.closeOnce.Do(func() {
		if a.cancel != nil {
			a.cancel()
		}
		if a.bus != nil {
			a.bus.Close()
		}
		if a.queue != nil {
			a.queue.Close()
		}
	})
}

func (a *App) acquireStory(ctx context.Context, storyID string) (func(), error) {
	a.storyMu.Lock()
	if a.storyGates == nil {
		a.storyGates = map[string]*storyGate{}
	}
	gate := a.storyGates[storyID]
	if gate == nil {
		gate = &storyGate{token: make(chan struct{}, 1)}
		gate.token <- struct{}{}
		a.storyGates[storyID] = gate
	}
	gate.refs++
	a.storyMu.Unlock()

	select {
	case <-ctx.Done():
		a.releaseStoryGate(storyID, gate)
		return nil, ctx.Err()
	case <-gate.token:
		if err := ctx.Err(); err != nil {
			gate.token <- struct{}{}
			a.releaseStoryGate(storyID, gate)
			return nil, err
		}
		var once sync.Once
		return func() {
			once.Do(func() {
				gate.token <- struct{}{}
				a.releaseStoryGate(storyID, gate)
			})
		}, nil
	}
}

func (a *App) releaseStoryGate(storyID string, gate *storyGate) {
	a.storyMu.Lock()
	defer a.storyMu.Unlock()
	gate.refs--
	if gate.refs == 0 && a.storyGates[storyID] == gate {
		delete(a.storyGates, storyID)
	}
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()
	api := http.NewServeMux()

	api.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "version": Version})
	})
	a.mountAuth(api)
	a.mountUsers(api)
	a.mountStories(api)
	a.mountConfig(api)
	a.mountUpdate(api)

	mux.Handle("/api/", http.StripPrefix("/api", a.cors(a.attachUser(api))))
	mux.HandleFunc("/", a.serveAsset)
	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("content-security-policy", "default-src 'self'; base-uri 'none'; object-src 'none'; frame-ancestors 'none'; form-action 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self' data:; connect-src 'self'")
		w.Header().Set("cross-origin-opener-policy", "same-origin")
		w.Header().Set("cross-origin-resource-policy", "same-origin")
		w.Header().Set("permissions-policy", "camera=(), microphone=(), geolocation=()")
		w.Header().Set("referrer-policy", "no-referrer")
		w.Header().Set("x-content-type-options", "nosniff")
		w.Header().Set("x-frame-options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("cache-control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("origin"))
		if origin != "" {
			w.Header().Add("vary", "Origin")
			if !allowedRequestOrigin(origin, r.Host) {
				writeError(w, http.StatusForbidden, "cross-origin request denied")
				return
			}
			w.Header().Set("access-control-allow-origin", origin)
			w.Header().Set("access-control-allow-credentials", "true")
			w.Header().Set("access-control-allow-headers", "content-type, authorization")
			w.Header().Set("access-control-allow-methods", "GET,POST,PUT,DELETE,OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func allowedRequestOrigin(origin, requestHost string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	if strings.EqualFold(parsed.Host, requestHost) {
		return true
	}
	return false
}

func (a *App) mountAuth(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/me", func(w http.ResponseWriter, r *http.Request) {
		count, err := a.store.UserCount()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "数据存储不可用")
			return
		}
		var user *PublicUser
		if cur := currentUser(r); cur != nil {
			pub := publicUser(*cur)
			user = &pub
		}
		writeJSON(w, http.StatusOK, AuthMe{User: user, NeedsSetup: count == 0})
	})
	mux.HandleFunc("GET /auth/setup-status", func(w http.ResponseWriter, r *http.Request) {
		count, err := a.store.UserCount()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "数据存储不可用")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"needsSetup": count == 0})
	})
	mux.HandleFunc("POST /auth/setup", func(w http.ResponseWriter, r *http.Request) {
		count, err := a.store.UserCount()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "数据存储不可用")
			return
		}
		if count > 0 {
			writeError(w, http.StatusConflict, "已完成初始化")
			return
		}
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &body); err != nil || validateUsernamePassword(body.Username, body.Password) != nil {
			writeError(w, http.StatusBadRequest, "参数无效")
			return
		}
		finishAttempt, err := a.authLoginGuard().Begin(r.Context(), loginAttemptKey(r, "__setup__"))
		if err != nil {
			writeLoginGuardError(w, err)
			return
		}
		defer finishAttempt(true)
		hash, err := hashPassword(body.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		record, err := a.store.CreateInitialAdmin(body.Username, hash)
		if err != nil {
			if errors.Is(err, errSetupComplete) {
				writeError(w, http.StatusConflict, err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, "数据存储不可用")
			}
			return
		}
		if err := a.startSession(w, r, record.ID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]PublicUser{"user": publicUser(*record)})
	})
	mux.HandleFunc("POST /auth/login", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &body); err != nil || validateUsernamePassword(body.Username, body.Password) != nil {
			writeError(w, http.StatusBadRequest, "用户名或密码无效")
			return
		}
		finishAttempt, err := a.authLoginGuard().Begin(r.Context(), loginAttemptKey(r, body.Username))
		if err != nil {
			writeLoginGuardError(w, err)
			return
		}
		success := false
		defer func() { finishAttempt(success) }()
		record, err := a.store.FindUserByUsername(body.Username)
		if err != nil {
			success = true
			writeError(w, http.StatusInternalServerError, "数据存储不可用")
			return
		}
		passwordHash := dummyPasswordHash
		if record != nil {
			passwordHash = record.PasswordHash
		}
		if !verifyPassword(body.Password, passwordHash) || record == nil {
			writeError(w, http.StatusUnauthorized, "用户名或密码错误")
			return
		}
		success = true
		if err := a.startSession(w, r, record.ID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]PublicUser{"user": publicUser(*record)})
	})
	mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		if err := a.endSession(w, r); err != nil {
			writeError(w, http.StatusInternalServerError, "数据存储不可用")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
}

func (a *App) mountUsers(mux *http.ServeMux) {
	mux.HandleFunc("GET /users", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		users, err := a.store.ListPublicUsers()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "数据存储不可用")
			return
		}
		writeJSON(w, http.StatusOK, map[string][]PublicUser{"users": users})
	}))
	mux.HandleFunc("POST /users", requireAdmin(func(w http.ResponseWriter, r *http.Request, me *UserRecord) {
		var body struct {
			Username string `json:"username"`
			Password string `json:"password"`
			Role     Role   `json:"role"`
		}
		if err := decodeJSON(r, &body); err != nil || validateUsernamePassword(body.Username, body.Password) != nil {
			writeError(w, http.StatusBadRequest, "参数无效")
			return
		}
		if body.Role == "" {
			body.Role = RoleUser
		}
		if !validateRole(body.Role) {
			writeError(w, http.StatusBadRequest, "参数无效")
			return
		}
		hash, err := hashPassword(body.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		record, err := a.store.CreateUserAsAdmin(me.ID, body.Username, hash, body.Role)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]PublicUser{"user": publicUser(*record)})
	}))
	mux.HandleFunc("PUT /users/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, me *UserRecord) {
		id := r.PathValue("id")
		var body struct {
			Password string `json:"password"`
			Role     Role   `json:"role"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "参数无效")
			return
		}
		if body.Password != "" && (len(body.Password) < 6 || len(body.Password) > 200) {
			writeError(w, http.StatusBadRequest, "参数无效")
			return
		}
		if body.Role != "" {
			if !validateRole(body.Role) {
				writeError(w, http.StatusBadRequest, "参数无效")
				return
			}
		}
		hash := ""
		if body.Password != "" {
			var err error
			hash, err = hashPassword(body.Password)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		next, err := a.store.UpdateUserAsAdmin(me.ID, id, hash, body.Role)
		if err != nil {
			switch {
			case errors.Is(err, errUserNotFound):
				writeError(w, http.StatusNotFound, err.Error())
			case errors.Is(err, errCannotModifySelf), errors.Is(err, errLastAdmin):
				writeError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, errAdminRequired):
				writeError(w, http.StatusForbidden, err.Error())
			default:
				writeError(w, http.StatusInternalServerError, "用户更新失败")
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]PublicUser{"user": publicUser(*next)})
	}))
	mux.HandleFunc("DELETE /users/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, me *UserRecord) {
		id := r.PathValue("id")
		if err := a.store.DeleteUserAsAdmin(me.ID, id); err != nil {
			switch {
			case errors.Is(err, errUserNotFound):
				writeError(w, http.StatusNotFound, err.Error())
			case errors.Is(err, errCannotDeleteSelf), errors.Is(err, errLastAdmin):
				writeError(w, http.StatusBadRequest, err.Error())
			case errors.Is(err, errAdminRequired):
				writeError(w, http.StatusForbidden, err.Error())
			default:
				writeError(w, http.StatusInternalServerError, "用户删除失败")
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
}

func (a *App) mountStories(mux *http.ServeMux) {
	mux.HandleFunc("GET /stories", func(w http.ResponseWriter, r *http.Request) {
		idx, err := a.store.LoadIndex()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := StoryList{Stories: make([]StoryListItem, 0, len(idx.Stories))}
		for _, entry := range idx.Stories {
			item := StoryListItem{IndexEntry: entry}
			if entry.Status != StatusReady {
				p, err := a.store.LoadProgress(entry.ID)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				if p != nil {
					item.Progress = p
				}
			}
			out.Stories = append(out.Stories, item)
		}
		writeJSON(w, http.StatusOK, out)
	})
	mux.HandleFunc("POST /stories", requireAuth(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		var body struct {
			URL  string          `json:"url"`
			Mode TranslationMode `json:"mode"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid url")
			return
		}
		if _, err := url.ParseRequestURI(body.URL); err != nil || body.URL == "" {
			writeError(w, http.StatusBadRequest, "invalid url")
			return
		}
		out, err := a.CreateFromURL(body.URL, body.Mode)
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, errInvalidAO3WorkURL), errors.Is(err, errInvalidTranslationMode):
				status = http.StatusBadRequest
			case errors.Is(err, errImportTooLarge):
				status = http.StatusRequestEntityTooLarge
			case errors.Is(err, errAO3Upstream):
				status = http.StatusBadGateway
			case errors.Is(err, errQueueClosed), errors.Is(err, errQueueFull):
				status = http.StatusServiceUnavailable
			}
			writeError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, out)
	}))
	mux.HandleFunc("POST /stories/upload", requireAuth(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		r.Body = http.MaxBytesReader(w, r.Body, maxUploadRequestBytes)
		html, mode, err := readUploadHTML(r)
		if err != nil {
			status := http.StatusBadRequest
			if requestTooLarge(err) {
				status = http.StatusRequestEntityTooLarge
			}
			writeError(w, status, "empty or invalid html")
			return
		}
		cfg, err := a.store.LoadConfig()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(html) < cfg.Import.MinHTMLLength {
			writeError(w, http.StatusBadRequest, "empty or invalid html")
			return
		}
		out, err := a.CreateFromHTML(html, mode)
		if err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, errImportTooLarge):
				status = http.StatusRequestEntityTooLarge
			case errors.Is(err, errInvalidImport), errors.Is(err, errInvalidTranslationMode):
				status = http.StatusBadRequest
			case errors.Is(err, errQueueClosed), errors.Is(err, errQueueFull):
				status = http.StatusServiceUnavailable
			}
			writeError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, out)
	}))
	mux.HandleFunc("GET /stories/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if err := validateStoryID(id); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		meta, err := a.store.LoadMeta(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if meta == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		progress, err := a.store.LoadProgress(id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"meta": meta, "progress": progress})
	})
	mux.HandleFunc("GET /stories/{id}/chapters/{n}", func(w http.ResponseWriter, r *http.Request) {
		a.handleChapter(w, r)
	})
	mux.HandleFunc("POST /stories/{id}/retry", requireAuth(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		var body struct {
			BlockIDs     []string        `json:"blockIds"`
			ChapterIndex *int            `json:"chapterIndex"`
			Mode         TranslationMode `json:"mode"`
		}
		if err := decodeJSON(r, &body); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid retry payload")
			return
		}
		if err := a.RetryStory(r.PathValue("id"), body.BlockIDs, body.ChapterIndex, body.Mode); err != nil {
			status := http.StatusInternalServerError
			switch {
			case errors.Is(err, errStoryNotFound):
				status = http.StatusNotFound
			case errors.Is(err, errInvalidStoryID), errors.Is(err, errInvalidRetrySelection), errors.Is(err, errInvalidTranslationMode):
				status = http.StatusBadRequest
			case errors.Is(err, errQueueClosed), errors.Is(err, errQueueFull):
				status = http.StatusServiceUnavailable
			}
			writeError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("DELETE /stories/{id}", requireAuth(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		if err := a.DeleteStory(r.PathValue("id")); err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errInvalidStoryID) {
				status = http.StatusBadRequest
			}
			writeError(w, status, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /stories/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		a.handleStream(w, r)
	})
	mux.HandleFunc("GET /stories/{id}/translation-status", func(w http.ResponseWriter, r *http.Request) {
		a.handleTranslationStatus(w, r)
	})
	mux.HandleFunc("POST /stories/{id}/translation-status/reset", requireAuth(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		id := r.PathValue("id")
		if err := validateStoryID(id); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if !a.store.StoryExists(id) {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if err := a.store.ResetStats(id); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /stories/{id}/reanalyze", requireAuth(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		if err := a.ReanalyzeStory(r.PathValue("id")); err != nil {
			if errors.Is(err, errInvalidStoryID) {
				writeError(w, http.StatusBadRequest, err.Error())
			} else if errors.Is(err, errStoryNotFound) {
				writeError(w, http.StatusNotFound, "not found")
			} else if errors.Is(err, errQueueClosed) || errors.Is(err, errQueueFull) {
				writeError(w, http.StatusServiceUnavailable, err.Error())
			} else {
				writeError(w, http.StatusInternalServerError, err.Error())
			}
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
}

func (a *App) handleTranslationStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := validateStoryID(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !a.store.StoryExists(id) {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	stats, err := a.store.LoadStats(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	transCtx, err := a.store.LoadContext(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	meta, err := a.store.LoadMeta(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	mode := TranslationModeNormal
	if meta != nil && meta.TranslationMode != "" {
		mode = meta.TranslationMode
	}

	user := currentUser(r)
	if user == nil {
		stats.Events = []LLMCallEvent{}
		stats.Samples = map[LLMCallStage]RequestSample{}
	}

	writeJSON(w, http.StatusOK, TranslationStatusView{
		Stats:   stats.Stats,
		Events:  stats.Events,
		Samples: stats.Samples,
		Context: transCtx,
		Mode:    mode,
	})
}

func readUploadHTML(r *http.Request) (string, TranslationMode, error) {
	ct := r.Header.Get("content-type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		reader, err := r.MultipartReader()
		if err != nil {
			return "", "", err
		}
		var html string
		var mode TranslationMode
		seenFile := false
		partCount := 0
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return "", "", err
			}
			partCount++
			if partCount > maxUploadParts {
				_ = part.Close()
				return "", "", errors.New("too many multipart fields")
			}
			switch part.FormName() {
			case "file", "html":
				if seenFile {
					_ = part.Close()
					return "", "", errors.New("multiple html files")
				}
				data, err := readMultipartPart(part, maxUploadHTMLBytes)
				if err != nil {
					return "", "", err
				}
				seenFile = true
				html = string(data)
			case "mode":
				data, err := readMultipartPart(part, 128)
				if err != nil {
					return "", "", err
				}
				mode = TranslationMode(strings.TrimSpace(string(data)))
			default:
				_ = part.Close()
			}
		}
		if html == "" {
			return "", "", errors.New("no file")
		}
		return html, mode, nil
	}
	data, err := readLimited(r.Body, maxUploadHTMLBytes)
	if err != nil {
		return "", "", err
	}
	return string(data), TranslationMode(strings.TrimSpace(r.URL.Query().Get("mode"))), nil
}

func readMultipartPart(part *multipart.Part, maxBytes int64) ([]byte, error) {
	defer part.Close()
	return readLimited(part, maxBytes)
}

func readLimited(reader io.Reader, maxBytes int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > maxBytes {
		return nil, errRequestTooLarge
	}
	return data, nil
}

func requestTooLarge(err error) bool {
	var maxBytesErr *http.MaxBytesError
	return errors.Is(err, errRequestTooLarge) || errors.As(err, &maxBytesErr)
}

func (a *App) handleChapter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := validateStoryID(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 {
		writeError(w, http.StatusBadRequest, "invalid chapter index")
		return
	}
	meta, err := a.store.LoadMeta(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	original, err := a.store.LoadOriginal(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	translated, err := a.store.LoadTranslated(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	progress, err := a.store.LoadProgress(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if meta == nil || original == nil || translated == nil || progress == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	if n >= len(original.Chapters) {
		writeError(w, http.StatusNotFound, "chapter out of range")
		return
	}
	oCh := original.Chapters[n]
	tCh := Chapter{}
	if n < len(translated.Chapters) {
		tCh = translated.Chapters[n]
	}
	view := ChapterView{Meta: *meta, Progress: *progress}
	view.Chapter.Index = n
	view.Chapter.TitleEn = oCh.Title
	view.Chapter.TitleZH = tCh.Title
	view.Chapter.Pairs = []ChapterPair{}
	for i, block := range oCh.Blocks {
		pair := ChapterPair{ID: block.ID, Type: block.Type, En: block.HTML, Status: BlockPending}
		if i < len(tCh.Blocks) {
			tb := tCh.Blocks[i]
			pair.ZH = tb.HTML
			if tb.Status != "" {
				pair.Status = tb.Status
			}
			pair.Error = tb.Error
		}
		view.Chapter.Pairs = append(view.Chapter.Pairs, pair)
	}
	if n > 0 {
		prev := n - 1
		view.Nav.Prev = &prev
	}
	if n+1 < len(original.Chapters) {
		next := n + 1
		view.Nav.Next = &next
	}
	view.Nav.Total = len(original.Chapters)
	writeJSON(w, http.StatusOK, view)
}

func (a *App) handleStream(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := validateStoryID(id); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !a.store.StoryExists(id) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	ch, unsubscribe := a.bus.Subscribe(id)
	defer unsubscribe()
	progress, err := a.store.LoadProgress(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if progress == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	cfg, err := a.store.LoadConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("content-type", "text/event-stream")
	w.Header().Set("cache-control", "no-cache")
	w.Header().Set("connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	writeSSE := func(event string, data any) bool {
		var text string
		switch v := data.(type) {
		case string:
			text = v
		default:
			buf, err := json.Marshal(v)
			if err != nil {
				return false
			}
			text = string(buf)
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, text); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !writeSSE("progress", StreamEvent{Type: "progress", DoneBlocks: progress.DoneBlocks, TotalBlocks: progress.TotalBlocks, Phase: progress.Phase}) ||
		!writeSSE("phase", StreamEvent{Type: "phase", Phase: progress.Phase}) {
		return
	}
	heartbeat := cfg.Stream.HeartbeatMS
	if heartbeat <= 0 {
		heartbeat = 15000
	}
	ticker := time.NewTicker(time.Duration(heartbeat) * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !writeSSE("ping", strconv.FormatInt(time.Now().UnixMilli(), 10)) {
				return
			}
		case event, ok := <-ch:
			if !ok {
				return
			}
			if !writeSSE(event.Type, event) {
				return
			}
		}
	}
}

func (a *App) mountConfig(mux *http.ServeMux) {
	mux.HandleFunc("GET /config/public", func(w http.ResponseWriter, r *http.Request) {
		cfg, err := a.store.LoadConfig()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"reader": cfg.Reader,
			"ui":     cfg.UI,
			"llm": map[string]any{
				"mode": cfg.LLM.Mode,
			},
		})
	})
	mux.HandleFunc("GET /config", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		cfg, err := a.store.LoadConfig()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		body := configResponse(cfg)
		writeJSON(w, http.StatusOK, body)
	}))
	mux.HandleFunc("PUT /config", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		current, err := a.store.LoadConfig()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var raw map[string]json.RawMessage
		if err := decodeJSON(r, &raw); err != nil {
			writeError(w, http.StatusBadRequest, "invalid config")
			return
		}
		merged := current
		mergeConfig(&merged, raw)
		if err := a.store.SaveConfig(merged); err != nil {
			writeError(w, http.StatusBadRequest, "invalid config")
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("POST /config/test", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		cfg, err := a.store.LoadConfig()
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		result, err := chat(r.Context(), cfg.LLM, []ChatMessage{
			{Role: "system", Content: "Echo the user input as JSON {\"ok\":true}."},
			{Role: "user", Content: "ping"},
		}, true)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "content": truncate(result.Content, 200), "usage": result.Usage})
	}))
}

func configResponse(cfg Config) map[string]any {
	return map[string]any{
		"server": cfg.Server,
		"auth":   cfg.Auth,
		"stream": cfg.Stream,
		"import": cfg.Import,
		"ui":     cfg.UI,
		"llm": map[string]any{
			"apiType":                cfg.LLM.APIType,
			"baseURL":                cfg.LLM.BaseURL,
			"apiKey":                 maskSecret(cfg.LLM.APIKey),
			"hasApiKey":              cfg.LLM.APIKey != "",
			"model":                  cfg.LLM.Model,
			"temperature":            cfg.LLM.Temperature,
			"concurrency":            cfg.LLM.Concurrency,
			"blocksPerRequest":       cfg.LLM.BlocksPerRequest,
			"maxTokensPerRequest":    cfg.LLM.MaxTokensPerRequest,
			"maxAutoRetries":         cfg.LLM.MaxAutoRetries,
			"mode":                   cfg.LLM.Mode,
			"analysisMaxInputTokens": cfg.LLM.AnalysisMaxInputTokens,
			"stream":                 cfg.LLM.Stream,
		},
		"ao3": map[string]any{
			"cookie":    map[bool]string{true: "***", false: ""}[cfg.AO3.Cookie != ""],
			"hasCookie": cfg.AO3.Cookie != "",
			"userAgent": cfg.AO3.UserAgent,
		},
		"reader": cfg.Reader,
		"update": cfg.Update,
	}
}

func mergeConfig(cfg *Config, raw map[string]json.RawMessage) {
	if v, ok := raw["server"]; ok {
		_ = json.Unmarshal(v, &cfg.Server)
	}
	if v, ok := raw["auth"]; ok {
		_ = json.Unmarshal(v, &cfg.Auth)
	}
	if v, ok := raw["stream"]; ok {
		_ = json.Unmarshal(v, &cfg.Stream)
	}
	if v, ok := raw["import"]; ok {
		_ = json.Unmarshal(v, &cfg.Import)
	}
	if v, ok := raw["ui"]; ok {
		_ = json.Unmarshal(v, &cfg.UI)
	}
	if v, ok := raw["reader"]; ok {
		_ = json.Unmarshal(v, &cfg.Reader)
	}
	if v, ok := raw["update"]; ok {
		_ = json.Unmarshal(v, &cfg.Update)
	}
	if v, ok := raw["llm"]; ok {
		previous := cfg.LLM
		currentKey := previous.APIKey
		var patch map[string]json.RawMessage
		_ = json.Unmarshal(v, &patch)
		_ = json.Unmarshal(v, &cfg.LLM)
		_, hasAPIKeyPatch := patch["apiKey"]
		if !hasAPIKeyPatch ||
			(currentKey != "" && cfg.LLM.APIKey == maskSecret(currentKey)) {
			cfg.LLM.APIKey = currentKey
		}
		normalizeLLMProviderDefaults(&cfg.LLM, previous, patch)
	}
	if v, ok := raw["ao3"]; ok {
		currentCookie := cfg.AO3.Cookie
		var patch map[string]json.RawMessage
		_ = json.Unmarshal(v, &patch)
		_ = json.Unmarshal(v, &cfg.AO3)
		_, hasCookiePatch := patch["cookie"]
		if !hasCookiePatch ||
			(currentCookie != "" && cfg.AO3.Cookie == "***") {
			cfg.AO3.Cookie = currentCookie
		}
	}
	*cfg = normalizeConfig(*cfg)
}

func (a *App) mountUpdate(mux *http.ServeMux) {
	mux.HandleFunc("GET /update/version", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, a.VersionInfo())
	})
	mux.HandleFunc("POST /update/check", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		info, err := a.CheckUpdate(r.Context())
		if err != nil {
			writeError(w, http.StatusBadGateway, "检查更新失败: "+err.Error())
			return
		}
		writeJSON(w, http.StatusOK, info)
	}))
	mux.HandleFunc("POST /update/apply", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		var body struct {
			Force        bool   `json:"force"`
			ForceVersion string `json:"forceVersion"`
			Version      string `json:"version"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "参数无效")
			return
		}
		forceVersion := strings.TrimSpace(body.ForceVersion)
		if forceVersion == "" {
			forceVersion = strings.TrimSpace(body.Version)
		}
		result := a.ApplyUpdate(ApplyUpdateOptions{
			Force:        body.Force,
			ForceVersion: forceVersion,
		})
		status := http.StatusBadRequest
		if result.OK {
			status = http.StatusOK
			if result.Restart {
				cfg, _ := a.store.LoadConfig()
				scheduleExec(cfg.Update.RestartDelayMS, result.execPath)
			}
		}
		writeJSON(w, status, result)
	}))
}

func (a *App) serveAsset(w http.ResponseWriter, r *http.Request) {
	clean := path.Clean("/" + r.URL.Path)
	key := strings.TrimPrefix(clean, "/")
	if key == "" {
		key = "index.html"
	}
	full := path.Join(webassets.Root, key)
	if serveEmbeddedFile(w, r, full) {
		return
	}
	if !strings.Contains(path.Base(key), ".") || strings.HasSuffix(key, ".html") {
		if serveEmbeddedFile(w, r, path.Join(webassets.Root, "index.html")) {
			return
		}
	}
	if !embeddedHasIndex() {
		http.Error(w, "AO3-Hub server is running. Web bundle is not embedded; run `npm run dev:web` or build the server.", http.StatusNotFound)
		return
	}
	http.Error(w, "not found", http.StatusNotFound)
}

func serveEmbeddedFile(w http.ResponseWriter, r *http.Request, name string) bool {
	file, err := webassets.FS.Open(name)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	ext := path.Ext(name)
	if mt := mime.TypeByExtension(ext); mt != "" {
		w.Header().Set("content-type", mt)
	}
	http.ServeContent(w, r, path.Base(name), info.ModTime(), file.(io.ReadSeeker))
	return true
}

func embeddedHasIndex() bool {
	_, err := fs.Stat(webassets.FS, path.Join(webassets.Root, "index.html"))
	return err == nil
}
