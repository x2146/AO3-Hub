package app

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Store struct {
	dir string
	mu  sync.Mutex
}

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(parts ...string) string {
	all := append([]string{s.dir}, parts...)
	return filepath.Join(all...)
}

func (s *Store) storyDir(id string) string {
	return s.path("stories", id)
}

func (s *Store) readJSON(path string, dst any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return false, nil
	}
	return true, nil
}

func (s *Store) writeJSON(path string, data any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) writeText(path string, data string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(data), 0o644)
}

func (s *Store) readText(path string) (string, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return string(data), true, nil
}

func (s *Store) LoadConfig() (Config, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var cfg Config
	ok, err := s.readJSON(s.path("config.json"), &cfg)
	if err != nil {
		return Config{}, err
	}
	if !ok {
		cfg = defaultConfig()
		return cfg, s.writeJSON(s.path("config.json"), cfg)
	}
	cfg = normalizeConfig(cfg)
	_ = s.writeJSON(s.path("config.json"), cfg)
	return cfg, nil
}

func (s *Store) SaveConfig(cfg Config) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateConfig(cfg); err != nil {
		return err
	}
	return s.writeJSON(s.path("config.json"), normalizeConfig(cfg))
}

func (s *Store) LoadIndex() (IndexFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var idx IndexFile
	ok, err := s.readJSON(s.path("index.json"), &idx)
	if err != nil {
		return IndexFile{}, err
	}
	if !ok || idx.Stories == nil {
		return IndexFile{Stories: []IndexEntry{}}, nil
	}
	return idx, nil
}

func (s *Store) SaveIndex(idx IndexFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if idx.Stories == nil {
		idx.Stories = []IndexEntry{}
	}
	return s.writeJSON(s.path("index.json"), idx)
}

func (s *Store) UpsertIndex(entry IndexEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var idx IndexFile
	_, _ = s.readJSON(s.path("index.json"), &idx)
	if idx.Stories == nil {
		idx.Stories = []IndexEntry{}
	}
	for i := range idx.Stories {
		if idx.Stories[i].ID == entry.ID {
			idx.Stories[i] = entry
			return s.writeJSON(s.path("index.json"), idx)
		}
	}
	idx.Stories = append([]IndexEntry{entry}, idx.Stories...)
	return s.writeJSON(s.path("index.json"), idx)
}

func (s *Store) PatchIndex(id string, patch func(*IndexEntry)) (*IndexEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var idx IndexFile
	_, _ = s.readJSON(s.path("index.json"), &idx)
	for i := range idx.Stories {
		if idx.Stories[i].ID != id {
			continue
		}
		patch(&idx.Stories[i])
		idx.Stories[i].UpdatedAt = nowISO()
		if err := s.writeJSON(s.path("index.json"), idx); err != nil {
			return nil, err
		}
		out := idx.Stories[i]
		return &out, nil
	}
	return nil, nil
}

func (s *Store) StoryExists(id string) bool {
	info, err := os.Stat(s.storyDir(id))
	return err == nil && info.IsDir()
}

func (s *Store) LoadMeta(id string) (*Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var meta Meta
	ok, err := s.readJSON(s.path("stories", id, "meta.json"), &meta)
	if err != nil || !ok {
		return nil, err
	}
	meta = normalizeMeta(meta)
	return &meta, nil
}

func (s *Store) SaveMeta(id string, meta Meta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeJSON(s.path("stories", id, "meta.json"), normalizeMeta(meta))
}

func (s *Store) LoadOriginal(id string) (*ChapterFile, error) {
	return s.loadChapterFile(s.path("stories", id, "original.json"))
}

func (s *Store) SaveOriginal(id string, file ChapterFile) error {
	return s.saveChapterFile(s.path("stories", id, "original.json"), file)
}

func (s *Store) LoadTranslated(id string) (*ChapterFile, error) {
	return s.loadChapterFile(s.path("stories", id, "translated.json"))
}

func (s *Store) SaveTranslated(id string, file ChapterFile) error {
	return s.saveChapterFile(s.path("stories", id, "translated.json"), file)
}

func (s *Store) loadChapterFile(path string) (*ChapterFile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var file ChapterFile
	ok, err := s.readJSON(path, &file)
	if err != nil || !ok {
		return nil, err
	}
	if file.Chapters == nil {
		file.Chapters = []Chapter{}
	}
	for i := range file.Chapters {
		if file.Chapters[i].Blocks == nil {
			file.Chapters[i].Blocks = []Block{}
		}
	}
	return &file, nil
}

func (s *Store) saveChapterFile(path string, file ChapterFile) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if file.Chapters == nil {
		file.Chapters = []Chapter{}
	}
	return s.writeJSON(path, file)
}

func (s *Store) LoadProgress(id string) (*Progress, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var progress Progress
	ok, err := s.readJSON(s.path("stories", id, "progress.json"), &progress)
	if err != nil || !ok {
		return nil, err
	}
	progress = normalizeProgress(progress)
	return &progress, nil
}

func (s *Store) SaveProgress(id string, progress Progress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeJSON(s.path("stories", id, "progress.json"), normalizeProgress(progress))
}

func (s *Store) SaveSource(id string, html string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeText(s.path("stories", id, "source.html"), html)
}

func (s *Store) LoadSource(id string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readText(s.path("stories", id, "source.html"))
}

func (s *Store) LoadContext(id string) (*TranslationContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var ctx TranslationContext
	ok, err := s.readJSON(s.path("stories", id, "context.json"), &ctx)
	if err != nil || !ok {
		return nil, err
	}
	if ctx.Ships == nil {
		ctx.Ships = []string{}
	}
	if ctx.Characters == nil {
		ctx.Characters = []Character{}
	}
	if ctx.Glossary == nil {
		ctx.Glossary = map[string]string{}
	}
	if ctx.ChapterSummaries == nil {
		ctx.ChapterSummaries = []ChapterSummary{}
	}
	return &ctx, nil
}

func (s *Store) SaveContext(id string, ctx TranslationContext) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Ships == nil {
		ctx.Ships = []string{}
	}
	if ctx.Characters == nil {
		ctx.Characters = []Character{}
	}
	if ctx.Glossary == nil {
		ctx.Glossary = map[string]string{}
	}
	if ctx.ChapterSummaries == nil {
		ctx.ChapterSummaries = []ChapterSummary{}
	}
	return s.writeJSON(s.path("stories", id, "context.json"), ctx)
}

func (s *Store) DeleteContext(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := s.path("stories", id, "context.json")
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) RemoveStory(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.RemoveAll(s.storyDir(id))
}

func (s *Store) loadUsersLocked() UsersFile {
	var file UsersFile
	_, _ = s.readJSON(s.path("users.json"), &file)
	if file.Users == nil {
		file.Users = []UserRecord{}
	}
	return file
}

func (s *Store) saveUsersLocked(file UsersFile) error {
	if file.Users == nil {
		file.Users = []UserRecord{}
	}
	return s.writeJSON(s.path("users.json"), file)
}

func (s *Store) UserCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.loadUsersLocked().Users)
}

func (s *Store) ListPublicUsers() []PublicUser {
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.loadUsersLocked()
	out := make([]PublicUser, 0, len(file.Users))
	for _, u := range file.Users {
		out = append(out, publicUser(u))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out
}

func (s *Store) FindUserByID(id string) *UserRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.loadUsersLocked()
	for _, u := range file.Users {
		if u.ID == id {
			out := u
			return &out
		}
	}
	return nil
}

func (s *Store) FindUserByUsername(username string) *UserRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.loadUsersLocked()
	lower := strings.ToLower(username)
	for _, u := range file.Users {
		if strings.ToLower(u.Username) == lower {
			out := u
			return &out
		}
	}
	return nil
}

func (s *Store) CreateUser(username, passwordHash string, role Role) (*UserRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.loadUsersLocked()
	lower := strings.ToLower(username)
	for _, u := range file.Users {
		if strings.ToLower(u.Username) == lower {
			return nil, errors.New("用户名已存在")
		}
	}
	id, err := randomUserID()
	if err != nil {
		return nil, err
	}
	now := nowISO()
	record := UserRecord{
		ID:           id,
		Username:     username,
		PasswordHash: passwordHash,
		Role:         role,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	file.Users = append(file.Users, record)
	if err := s.saveUsersLocked(file); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Store) UpdateUser(id string, patch func(*UserRecord)) (*UserRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.loadUsersLocked()
	for i := range file.Users {
		if file.Users[i].ID != id {
			continue
		}
		patch(&file.Users[i])
		file.Users[i].UpdatedAt = nowISO()
		if err := s.saveUsersLocked(file); err != nil {
			return nil, err
		}
		out := file.Users[i]
		return &out, nil
	}
	return nil, nil
}

func (s *Store) RemoveUser(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.loadUsersLocked()
	next := file.Users[:0]
	removed := false
	for _, u := range file.Users {
		if u.ID == id {
			removed = true
			continue
		}
		next = append(next, u)
	}
	if !removed {
		return false
	}
	file.Users = next
	_ = s.saveUsersLocked(file)
	return true
}

func (s *Store) AdminCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.loadUsersLocked()
	count := 0
	for _, u := range file.Users {
		if u.Role == RoleAdmin {
			count++
		}
	}
	return count
}

func (s *Store) loadSessionsLocked() SessionsFile {
	var file SessionsFile
	_, _ = s.readJSON(s.path("sessions.json"), &file)
	if file.Sessions == nil {
		file.Sessions = []SessionRecord{}
	}
	return file
}

func (s *Store) saveSessionsLocked(file SessionsFile) error {
	if file.Sessions == nil {
		file.Sessions = []SessionRecord{}
	}
	return s.writeJSON(s.path("sessions.json"), file)
}

func sessionExpired(session SessionRecord, now time.Time) bool {
	t, err := time.Parse(time.RFC3339Nano, session.ExpiresAt)
	if err != nil {
		t, err = time.Parse(time.RFC3339, session.ExpiresAt)
	}
	return err != nil || !t.After(now)
}

func (s *Store) CreateSession(userID string, ttl time.Duration) (*SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.loadSessionsLocked()
	now := time.Now().UTC()
	active := file.Sessions[:0]
	for _, session := range file.Sessions {
		if !sessionExpired(session, now) {
			active = append(active, session)
		}
	}
	token, err := randomHex(32)
	if err != nil {
		return nil, err
	}
	created := now.Format(time.RFC3339Nano)
	record := SessionRecord{
		Token:      token,
		UserID:     userID,
		CreatedAt:  created,
		LastUsedAt: created,
		ExpiresAt:  now.Add(ttl).Format(time.RFC3339Nano),
	}
	file.Sessions = append(active, record)
	if err := s.saveSessionsLocked(file); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Store) FindValidSession(token string) *SessionRecord {
	if token == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.loadSessionsLocked()
	now := time.Now().UTC()
	for _, session := range file.Sessions {
		if session.Token == token && !sessionExpired(session, now) {
			out := session
			return &out
		}
	}
	return nil
}

func (s *Store) TouchSession(token string, ttl time.Duration) *SessionRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.loadSessionsLocked()
	now := time.Now().UTC()
	active := file.Sessions[:0]
	var touched *SessionRecord
	for _, session := range file.Sessions {
		if sessionExpired(session, now) {
			continue
		}
		if session.Token == token {
			session.LastUsedAt = now.Format(time.RFC3339Nano)
			session.ExpiresAt = now.Add(ttl).Format(time.RFC3339Nano)
			out := session
			touched = &out
		}
		active = append(active, session)
	}
	file.Sessions = active
	_ = s.saveSessionsLocked(file)
	return touched
}

func (s *Store) RemoveSession(token string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.loadSessionsLocked()
	next := file.Sessions[:0]
	for _, session := range file.Sessions {
		if session.Token != token {
			next = append(next, session)
		}
	}
	file.Sessions = next
	_ = s.saveSessionsLocked(file)
}

func (s *Store) RemoveSessionsByUser(userID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file := s.loadSessionsLocked()
	next := file.Sessions[:0]
	for _, session := range file.Sessions {
		if session.UserID != userID {
			next = append(next, session)
		}
	}
	file.Sessions = next
	_ = s.saveSessionsLocked(file)
}
