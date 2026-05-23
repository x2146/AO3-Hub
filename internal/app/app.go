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
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"ao3hub/internal/webassets"
)

type App struct {
	store *Store
	bus   *EventBus
	queue *Queue
	ctx   context.Context
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
	app := &App{
		store: store,
		bus:   NewEventBus(),
		ctx:   context.Background(),
	}
	app.queue = NewQueue(app)
	return app, nil
}

func (a *App) Run() error {
	cfg, err := a.store.LoadConfig()
	if err != nil {
		return err
	}
	if err := a.ResumeOnStartup(); err != nil {
		return err
	}
	host := resolveHost(cfg.Server.Host)
	port, err := resolvePort(cfg.Server.Port)
	if err != nil {
		return err
	}
	addr := fmt.Sprintf("%s:%d", host, port)
	fmt.Printf("[ao3-hub] v%s listening on http://%s\n", Version, addr)
	return http.ListenAndServe(addr, a.routes())
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
	return mux
}

func (a *App) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("access-control-allow-origin", r.Header.Get("origin"))
		w.Header().Set("access-control-allow-credentials", "true")
		w.Header().Set("access-control-allow-headers", "content-type, authorization")
		w.Header().Set("access-control-allow-methods", "GET,POST,PUT,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *App) mountAuth(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/me", func(w http.ResponseWriter, r *http.Request) {
		var user *PublicUser
		if cur := currentUser(r); cur != nil {
			pub := publicUser(*cur)
			user = &pub
		}
		writeJSON(w, http.StatusOK, AuthMe{User: user, NeedsSetup: a.store.UserCount() == 0})
	})
	mux.HandleFunc("GET /auth/setup-status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]bool{"needsSetup": a.store.UserCount() == 0})
	})
	mux.HandleFunc("POST /auth/setup", func(w http.ResponseWriter, r *http.Request) {
		if a.store.UserCount() > 0 {
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
		hash, err := hashPassword(body.Password)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		record, err := a.store.CreateUser(body.Username, hash, RoleAdmin)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
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
		record := a.store.FindUserByUsername(body.Username)
		if record == nil || !verifyPassword(body.Password, record.PasswordHash) {
			writeError(w, http.StatusUnauthorized, "用户名或密码错误")
			return
		}
		if err := a.startSession(w, r, record.ID); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]PublicUser{"user": publicUser(*record)})
	})
	mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) {
		a.endSession(w, r)
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})
}

func (a *App) mountUsers(mux *http.ServeMux) {
	mux.HandleFunc("GET /users", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		writeJSON(w, http.StatusOK, map[string][]PublicUser{"users": a.store.ListPublicUsers()})
	}))
	mux.HandleFunc("POST /users", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
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
		record, err := a.store.CreateUser(body.Username, hash, body.Role)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]PublicUser{"user": publicUser(*record)})
	}))
	mux.HandleFunc("PUT /users/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		id := r.PathValue("id")
		target := a.store.FindUserByID(id)
		if target == nil {
			writeError(w, http.StatusNotFound, "用户不存在")
			return
		}
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
			if body.Role != target.Role && target.Role == RoleAdmin && a.store.AdminCount() <= 1 {
				writeError(w, http.StatusBadRequest, "至少保留一个 admin")
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
		next, err := a.store.UpdateUser(id, func(u *UserRecord) {
			if hash != "" {
				u.PasswordHash = hash
			}
			if body.Role != "" {
				u.Role = body.Role
			}
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if hash != "" {
			a.store.RemoveSessionsByUser(id)
		}
		if next == nil {
			writeJSON(w, http.StatusOK, map[string]any{"user": nil})
			return
		}
		writeJSON(w, http.StatusOK, map[string]PublicUser{"user": publicUser(*next)})
	}))
	mux.HandleFunc("DELETE /users/{id}", requireAdmin(func(w http.ResponseWriter, r *http.Request, me *UserRecord) {
		id := r.PathValue("id")
		if me.ID == id {
			writeError(w, http.StatusBadRequest, "不能删除自己")
			return
		}
		target := a.store.FindUserByID(id)
		if target == nil {
			writeError(w, http.StatusNotFound, "用户不存在")
			return
		}
		if target.Role == RoleAdmin && a.store.AdminCount() <= 1 {
			writeError(w, http.StatusBadRequest, "至少保留一个 admin")
			return
		}
		a.store.RemoveUser(id)
		a.store.RemoveSessionsByUser(id)
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
		writeJSON(w, http.StatusOK, idx)
	})
	mux.HandleFunc("POST /stories", requireAuth(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		var body struct {
			URL string `json:"url"`
		}
		if err := decodeJSON(r, &body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid url")
			return
		}
		if _, err := url.ParseRequestURI(body.URL); err != nil || body.URL == "" {
			writeError(w, http.StatusBadRequest, "invalid url")
			return
		}
		out, err := a.CreateFromURL(body.URL)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, out)
	}))
	mux.HandleFunc("POST /stories/upload", requireAuth(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		html, err := readUploadHTML(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "empty or invalid html")
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
		out, err := a.CreateFromHTML(html)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, out)
	}))
	mux.HandleFunc("GET /stories/{id}", func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		meta, err := a.store.LoadMeta(id)
		if err != nil || meta == nil {
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
			BlockIDs     []string `json:"blockIds"`
			ChapterIndex *int     `json:"chapterIndex"`
		}
		if err := decodeJSON(r, &body); err != nil && !errors.Is(err, io.EOF) {
			writeError(w, http.StatusBadRequest, "invalid retry payload")
			return
		}
		if body.ChapterIndex != nil && *body.ChapterIndex < 0 {
			writeError(w, http.StatusBadRequest, "invalid retry payload")
			return
		}
		if err := a.RetryStory(r.PathValue("id"), body.BlockIDs, body.ChapterIndex); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("DELETE /stories/{id}", requireAuth(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		if err := a.DeleteStory(r.PathValue("id")); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}))
	mux.HandleFunc("GET /stories/{id}/stream", func(w http.ResponseWriter, r *http.Request) {
		a.handleStream(w, r)
	})
}

func readUploadHTML(r *http.Request) (string, error) {
	ct := r.Header.Get("content-type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		reader, err := r.MultipartReader()
		if err != nil {
			return "", err
		}
		for {
			part, err := reader.NextPart()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return "", err
			}
			if part.FormName() != "file" && part.FormName() != "html" {
				continue
			}
			data, err := readMultipartPart(part)
			if err != nil {
				return "", err
			}
			return string(data), nil
		}
		return "", errors.New("no file")
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func readMultipartPart(part *multipart.Part) ([]byte, error) {
	defer part.Close()
	return io.ReadAll(part)
}

func (a *App) handleChapter(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	n, err := strconv.Atoi(r.PathValue("n"))
	if err != nil || n < 0 {
		writeError(w, http.StatusBadRequest, "invalid chapter index")
		return
	}
	meta, _ := a.store.LoadMeta(id)
	original, _ := a.store.LoadOriginal(id)
	translated, _ := a.store.LoadTranslated(id)
	progress, _ := a.store.LoadProgress(id)
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
	if !a.store.StoryExists(id) {
		http.Error(w, "not found", http.StatusNotFound)
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
			buf, _ := json.Marshal(v)
			text = string(buf)
		}
		if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, text); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if progress, _ := a.store.LoadProgress(id); progress != nil {
		writeSSE("progress", StreamEvent{Type: "progress", DoneBlocks: progress.DoneBlocks, TotalBlocks: progress.TotalBlocks, Phase: progress.Phase})
		writeSSE("phase", StreamEvent{Type: "phase", Phase: progress.Phase})
	}
	ch, unsubscribe := a.bus.Subscribe(id)
	defer unsubscribe()
	cfg, _ := a.store.LoadConfig()
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
		writeJSON(w, http.StatusOK, map[string]any{"reader": cfg.Reader, "ui": cfg.UI})
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
			"baseURL":             cfg.LLM.BaseURL,
			"apiKey":              maskSecret(cfg.LLM.APIKey),
			"hasApiKey":           cfg.LLM.APIKey != "",
			"model":               cfg.LLM.Model,
			"temperature":         cfg.LLM.Temperature,
			"concurrency":         cfg.LLM.Concurrency,
			"blocksPerRequest":    cfg.LLM.BlocksPerRequest,
			"maxTokensPerRequest": cfg.LLM.MaxTokensPerRequest,
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
		currentKey := cfg.LLM.APIKey
		_ = json.Unmarshal(v, &cfg.LLM)
		if cfg.LLM.APIKey == "" || strings.Contains(cfg.LLM.APIKey, "…") {
			cfg.LLM.APIKey = currentKey
		}
	}
	if v, ok := raw["ao3"]; ok {
		currentCookie := cfg.AO3.Cookie
		_ = json.Unmarshal(v, &cfg.AO3)
		if cfg.AO3.Cookie == "***" {
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
		writeJSON(w, http.StatusOK, a.VersionInfo())
	}))
	mux.HandleFunc("POST /update/apply", requireAdmin(func(w http.ResponseWriter, r *http.Request, _ *UserRecord) {
		var body struct {
			Force bool `json:"force"`
		}
		_ = decodeJSON(r, &body)
		result := a.ApplyUpdate(body.Force)
		status := http.StatusBadRequest
		if result.OK {
			status = http.StatusOK
			if result.Restart {
				cfg, _ := a.store.LoadConfig()
				scheduleExit(cfg.Update.RestartDelayMS)
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
		http.Error(w, "AO3-Hub server is running. Web bundle is not embedded — run `bun --cwd web run dev` or build the server.", http.StatusNotFound)
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
