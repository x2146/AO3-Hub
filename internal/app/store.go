package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
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

const (
	maxSessionsPerUser    = 20
	minSessionTouchWindow = time.Minute
	maxSessionTouchWindow = time.Hour
)

var (
	errSetupComplete    = errors.New("已完成初始化")
	errUserNotFound     = errors.New("用户不存在")
	errAdminRequired    = errors.New("需要管理员权限")
	errCannotModifySelf = errors.New("不能修改自己的角色")
	errCannotDeleteSelf = errors.New("不能删除自己")
	errLastAdmin        = errors.New("至少保留一个 admin")
	errStoryNotFound    = errors.New("story not found")
)

func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

func (s *Store) path(parts ...string) string {
	all := append([]string{s.dir}, parts...)
	return filepath.Join(all...)
}

func (s *Store) storyPath(id string, parts ...string) (string, error) {
	if err := validateStoryID(id); err != nil {
		return "", err
	}
	all := append([]string{"stories", id}, parts...)
	return s.path(all...), nil
}

func (s *Store) readJSON(path string, dst any) (bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if len(bytes.TrimSpace(data)) == 0 || bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return false, fmt.Errorf("decode %s: empty or null JSON", filepath.Base(path))
	}
	if err := json.Unmarshal(data, dst); err != nil {
		return false, fmt.Errorf("decode %s: %w", filepath.Base(path), err)
	}
	return true, nil
}

func (s *Store) writeJSON(path string, data any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func (s *Store) writeText(path string, data string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(data), 0o600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
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
	previous := cfg
	cfg = normalizeConfig(cfg)
	if err := validateConfig(cfg); err != nil {
		return Config{}, fmt.Errorf("invalid config.json: %w", err)
	}
	if cfg != previous {
		if err := s.writeJSON(s.path("config.json"), cfg); err != nil {
			return Config{}, fmt.Errorf("migrate config.json: %w", err)
		}
	}
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
	if _, err := s.readJSON(s.path("index.json"), &idx); err != nil {
		return err
	}
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
	if _, err := s.readJSON(s.path("index.json"), &idx); err != nil {
		return nil, err
	}
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
	path, err := s.storyPath(id)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func (s *Store) LoadMeta(id string) (*Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.storyPath(id, "meta.json")
	if err != nil {
		return nil, err
	}
	var meta Meta
	ok, err := s.readJSON(path, &meta)
	if err != nil || !ok {
		return nil, err
	}
	meta = normalizeMeta(meta)
	return &meta, nil
}

func (s *Store) SaveMeta(id string, meta Meta) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.storyPath(id, "meta.json")
	if err != nil {
		return err
	}
	return s.writeJSON(path, normalizeMeta(meta))
}

func (s *Store) LoadOriginal(id string) (*ChapterFile, error) {
	path, err := s.storyPath(id, "original.json")
	if err != nil {
		return nil, err
	}
	return s.loadChapterFile(path)
}

func (s *Store) SaveOriginal(id string, file ChapterFile) error {
	path, err := s.storyPath(id, "original.json")
	if err != nil {
		return err
	}
	return s.saveChapterFile(path, file)
}

func (s *Store) LoadTranslated(id string) (*ChapterFile, error) {
	path, err := s.storyPath(id, "translated.json")
	if err != nil {
		return nil, err
	}
	return s.loadChapterFile(path)
}

func (s *Store) SaveTranslated(id string, file ChapterFile) error {
	path, err := s.storyPath(id, "translated.json")
	if err != nil {
		return err
	}
	return s.saveChapterFile(path, file)
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
	path, err := s.storyPath(id, "progress.json")
	if err != nil {
		return nil, err
	}
	var progress Progress
	ok, err := s.readJSON(path, &progress)
	if err != nil || !ok {
		return nil, err
	}
	progress = normalizeProgress(progress)
	return &progress, nil
}

func (s *Store) SaveProgress(id string, progress Progress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.storyPath(id, "progress.json")
	if err != nil {
		return err
	}
	return s.writeJSON(path, normalizeProgress(progress))
}

func (s *Store) UpdateProgress(id string, update func(Progress) Progress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.storyPath(id, "progress.json")
	if err != nil {
		return err
	}
	var progress Progress
	ok, err := s.readJSON(path, &progress)
	if err != nil {
		return err
	}
	if !ok {
		return errors.New("progress not found")
	}
	progress = normalizeProgress(progress)
	return s.writeJSON(path, normalizeProgress(update(progress)))
}

func (s *Store) FinishStory(id string, phase ProgressPhase, status StoryStatus, message string) error {
	return s.setStoryState(id, phase, status, message, true)
}

func (s *Store) QueueStory(id string) error {
	return s.setStoryState(id, PhaseQueued, StatusQueued, "", false)
}

func (s *Store) ReconcileStoryState(id string, progress Progress, status StoryStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	storyPath, err := s.storyPath(id)
	if err != nil {
		return err
	}
	if info, err := os.Stat(storyPath); err != nil || !info.IsDir() {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return errStoryNotFound
	}

	indexPath := s.path("index.json")
	var index IndexFile
	if _, err := s.readJSON(indexPath, &index); err != nil {
		return err
	}
	found := false
	for i := range index.Stories {
		if index.Stories[i].ID != id {
			continue
		}
		index.Stories[i].Status = status
		index.Stories[i].UpdatedAt = nowISO()
		found = true
		break
	}
	if !found {
		return errStoryNotFound
	}

	progressPath, err := s.storyPath(id, "progress.json")
	if err != nil {
		return err
	}
	previous, readErr := os.ReadFile(progressPath)
	hadPrevious := readErr == nil
	if readErr != nil && !errors.Is(readErr, os.ErrNotExist) {
		return readErr
	}
	if err := s.writeJSON(progressPath, normalizeProgress(progress)); err != nil {
		return err
	}
	if err := s.writeJSON(indexPath, index); err != nil {
		var rollbackErr error
		if hadPrevious {
			rollbackErr = s.writeText(progressPath, string(previous))
		} else if removeErr := os.Remove(progressPath); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			rollbackErr = removeErr
		}
		if rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rollback progress: %w", rollbackErr))
		}
		return err
	}
	return nil
}

func (s *Store) setStoryState(id string, phase ProgressPhase, status StoryStatus, message string, finished bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	storyPath, err := s.storyPath(id)
	if err != nil {
		return err
	}
	if info, err := os.Stat(storyPath); err != nil || !info.IsDir() {
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return errStoryNotFound
	}

	progressPath, err := s.storyPath(id, "progress.json")
	if err != nil {
		return err
	}
	var previousProgress Progress
	ok, err := s.readJSON(progressPath, &previousProgress)
	if err != nil {
		return err
	}
	if !ok {
		return errStoryNotFound
	}
	previousProgress = normalizeProgress(previousProgress)
	nextProgress := previousProgress
	nextProgress.Phase = phase
	nextProgress.CurrentChapter = nil
	nextProgress.InflightBlocks = 0
	nextProgress.Message = message
	if finished {
		nextProgress.FinishedAt = nowISO()
	} else {
		nextProgress.FinishedAt = ""
	}

	indexPath := s.path("index.json")
	var previousIndex IndexFile
	if _, err := s.readJSON(indexPath, &previousIndex); err != nil {
		return err
	}
	nextIndex := previousIndex
	nextIndex.Stories = append([]IndexEntry(nil), previousIndex.Stories...)
	found := false
	for i := range nextIndex.Stories {
		if nextIndex.Stories[i].ID != id {
			continue
		}
		nextIndex.Stories[i].Status = status
		nextIndex.Stories[i].UpdatedAt = nowISO()
		found = true
		break
	}
	if !found {
		return errStoryNotFound
	}

	if err := s.writeJSON(progressPath, normalizeProgress(nextProgress)); err != nil {
		return err
	}
	if err := s.writeJSON(indexPath, nextIndex); err != nil {
		if rollbackErr := s.writeJSON(progressPath, previousProgress); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rollback progress: %w", rollbackErr))
		}
		return err
	}
	return nil
}

func (s *Store) SaveSource(id string, html string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.storyPath(id, "source.html")
	if err != nil {
		return err
	}
	return s.writeText(path, html)
}

func (s *Store) LoadSource(id string) (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.storyPath(id, "source.html")
	if err != nil {
		return "", false, err
	}
	return s.readText(path)
}

func (s *Store) LoadContext(id string) (*TranslationContext, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.storyPath(id, "context.json")
	if err != nil {
		return nil, err
	}
	var ctx TranslationContext
	ok, err := s.readJSON(path, &ctx)
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
	path, err := s.storyPath(id, "context.json")
	if err != nil {
		return err
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
	return s.writeJSON(path, ctx)
}

func (s *Store) DeleteContext(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.storyPath(id, "context.json")
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func (s *Store) RemoveStory(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	path, err := s.storyPath(id)
	if err != nil {
		return err
	}
	return os.RemoveAll(path)
}

func (s *Store) RemoveIndexEntry(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	indexPath := s.path("index.json")
	var previous IndexFile
	if _, err := s.readJSON(indexPath, &previous); err != nil {
		return err
	}
	next := IndexFile{Stories: make([]IndexEntry, 0, len(previous.Stories))}
	for _, entry := range previous.Stories {
		if entry.ID != id {
			next.Stories = append(next.Stories, entry)
		}
	}
	return s.writeJSON(indexPath, next)
}

func (s *Store) RemoveStoryAndIndex(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	storyPath, err := s.storyPath(id)
	if err != nil {
		return err
	}

	indexPath := s.path("index.json")
	var previous IndexFile
	if _, err := s.readJSON(indexPath, &previous); err != nil {
		return err
	}
	next := IndexFile{Stories: make([]IndexEntry, 0, len(previous.Stories))}
	for _, entry := range previous.Stories {
		if entry.ID != id {
			next.Stories = append(next.Stories, entry)
		}
	}
	if err := s.writeJSON(indexPath, next); err != nil {
		return err
	}
	if err := os.RemoveAll(storyPath); err != nil {
		if rollbackErr := s.writeJSON(indexPath, previous); rollbackErr != nil {
			return errors.Join(err, fmt.Errorf("rollback index: %w", rollbackErr))
		}
		return err
	}
	return nil
}

func (s *Store) loadUsersLocked() (UsersFile, error) {
	var file UsersFile
	_, err := s.readJSON(s.path("users.json"), &file)
	if err != nil {
		return UsersFile{}, err
	}
	if file.Users == nil {
		file.Users = []UserRecord{}
	}
	return file, nil
}

func (s *Store) saveUsersLocked(file UsersFile) error {
	if file.Users == nil {
		file.Users = []UserRecord{}
	}
	return s.writeJSON(s.path("users.json"), file)
}

func (s *Store) UserCount() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadUsersLocked()
	if err != nil {
		return 0, err
	}
	return len(file.Users), nil
}

func (s *Store) ListPublicUsers() ([]PublicUser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadUsersLocked()
	if err != nil {
		return nil, err
	}
	out := make([]PublicUser, 0, len(file.Users))
	for _, u := range file.Users {
		out = append(out, publicUser(u))
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].CreatedAt < out[j].CreatedAt
	})
	return out, nil
}

func (s *Store) FindUserByID(id string) (*UserRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadUsersLocked()
	if err != nil {
		return nil, err
	}
	for _, u := range file.Users {
		if u.ID == id {
			out := u
			return &out, nil
		}
	}
	return nil, nil
}

func (s *Store) FindUserByUsername(username string) (*UserRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadUsersLocked()
	if err != nil {
		return nil, err
	}
	lower := strings.ToLower(username)
	for _, u := range file.Users {
		if strings.ToLower(u.Username) == lower {
			out := u
			return &out, nil
		}
	}
	return nil, nil
}

func (s *Store) CreateUser(username, passwordHash string, role Role) (*UserRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadUsersLocked()
	if err != nil {
		return nil, err
	}
	return s.createUserLocked(file, username, passwordHash, role)
}

func (s *Store) CreateUserAsAdmin(actorID, username, passwordHash string, role Role) (*UserRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadUsersLocked()
	if err != nil {
		return nil, err
	}
	actorIndex := userIndex(file, actorID)
	if actorIndex < 0 || file.Users[actorIndex].Role != RoleAdmin {
		return nil, errAdminRequired
	}
	return s.createUserLocked(file, username, passwordHash, role)
}

func (s *Store) CreateInitialAdmin(username, passwordHash string) (*UserRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadUsersLocked()
	if err != nil {
		return nil, err
	}
	if len(file.Users) != 0 {
		return nil, errSetupComplete
	}
	return s.createUserLocked(file, username, passwordHash, RoleAdmin)
}

func (s *Store) createUserLocked(file UsersFile, username, passwordHash string, role Role) (*UserRecord, error) {
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

func userIndex(file UsersFile, id string) int {
	for i := range file.Users {
		if file.Users[i].ID == id {
			return i
		}
	}
	return -1
}

func adminCount(file UsersFile) int {
	count := 0
	for _, user := range file.Users {
		if user.Role == RoleAdmin {
			count++
		}
	}
	return count
}

func removeUserSessions(file SessionsFile, userID string) SessionsFile {
	next := file.Sessions[:0]
	for _, session := range file.Sessions {
		if session.UserID != userID {
			next = append(next, session)
		}
	}
	file.Sessions = next
	return file
}

func (s *Store) UpdateUserAsAdmin(actorID, targetID, passwordHash string, role Role) (*UserRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadUsersLocked()
	if err != nil {
		return nil, err
	}
	actorIndex := userIndex(file, actorID)
	if actorIndex < 0 || file.Users[actorIndex].Role != RoleAdmin {
		return nil, errAdminRequired
	}
	targetIndex := userIndex(file, targetID)
	if targetIndex < 0 {
		return nil, errUserNotFound
	}
	target := &file.Users[targetIndex]
	if role != "" && role != target.Role {
		if actorID == targetID {
			return nil, errCannotModifySelf
		}
		if target.Role == RoleAdmin && adminCount(file) <= 1 {
			return nil, errLastAdmin
		}
		target.Role = role
	}
	var sessions SessionsFile
	if passwordHash != "" {
		sessions, err = s.loadSessionsLocked()
		if err != nil {
			return nil, err
		}
		target.PasswordHash = passwordHash
	}
	target.UpdatedAt = nowISO()
	if passwordHash != "" {
		if err := s.saveSessionsLocked(removeUserSessions(sessions, targetID)); err != nil {
			return nil, err
		}
	}
	if err := s.saveUsersLocked(file); err != nil {
		return nil, err
	}
	out := *target
	return &out, nil
}

func (s *Store) DeleteUserAsAdmin(actorID, targetID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadUsersLocked()
	if err != nil {
		return err
	}
	actorIndex := userIndex(file, actorID)
	if actorIndex < 0 || file.Users[actorIndex].Role != RoleAdmin {
		return errAdminRequired
	}
	if actorID == targetID {
		return errCannotDeleteSelf
	}
	targetIndex := userIndex(file, targetID)
	if targetIndex < 0 {
		return errUserNotFound
	}
	if file.Users[targetIndex].Role == RoleAdmin && adminCount(file) <= 1 {
		return errLastAdmin
	}
	sessions, err := s.loadSessionsLocked()
	if err != nil {
		return err
	}
	file.Users = append(file.Users[:targetIndex], file.Users[targetIndex+1:]...)
	if err := s.saveUsersLocked(file); err != nil {
		return err
	}
	return s.saveSessionsLocked(removeUserSessions(sessions, targetID))
}

func (s *Store) loadSessionsLocked() (SessionsFile, error) {
	var file SessionsFile
	_, err := s.readJSON(s.path("sessions.json"), &file)
	if err != nil {
		return SessionsFile{}, err
	}
	if file.Sessions == nil {
		file.Sessions = []SessionRecord{}
	}
	return file, nil
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
	file, err := s.loadSessionsLocked()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	active := file.Sessions[:0]
	userSessions := 0
	for _, session := range file.Sessions {
		if !sessionExpired(session, now) {
			active = append(active, session)
			if session.UserID == userID {
				userSessions++
			}
		}
	}
	for userSessions >= maxSessionsPerUser {
		for i, session := range active {
			if session.UserID == userID {
				active = append(active[:i], active[i+1:]...)
				userSessions--
				break
			}
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

func (s *Store) FindValidSession(token string) (*SessionRecord, error) {
	if token == "" {
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadSessionsLocked()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	for _, session := range file.Sessions {
		if session.Token == token && !sessionExpired(session, now) {
			out := session
			return &out, nil
		}
	}
	return nil, nil
}

func (s *Store) TouchSession(token string, ttl time.Duration) (*SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadSessionsLocked()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	active := file.Sessions[:0]
	var touched *SessionRecord
	changed := false
	touchWindow := ttl / 10
	if touchWindow < minSessionTouchWindow {
		touchWindow = minSessionTouchWindow
	}
	if touchWindow > maxSessionTouchWindow {
		touchWindow = maxSessionTouchWindow
	}
	for _, session := range file.Sessions {
		if sessionExpired(session, now) {
			changed = true
			continue
		}
		if session.Token == token {
			lastUsed, err := time.Parse(time.RFC3339Nano, session.LastUsedAt)
			if err != nil || now.Sub(lastUsed) >= touchWindow {
				session.LastUsedAt = now.Format(time.RFC3339Nano)
				session.ExpiresAt = now.Add(ttl).Format(time.RFC3339Nano)
				changed = true
			}
			out := session
			touched = &out
		}
		active = append(active, session)
	}
	file.Sessions = active
	if changed {
		if err := s.saveSessionsLocked(file); err != nil {
			return nil, err
		}
	}
	return touched, nil
}

func (s *Store) RemoveSession(token string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadSessionsLocked()
	if err != nil {
		return err
	}
	next := file.Sessions[:0]
	for _, session := range file.Sessions {
		if session.Token != token {
			next = append(next, session)
		}
	}
	file.Sessions = next
	return s.saveSessionsLocked(file)
}

func (s *Store) RemoveSessionsByUser(userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := s.loadSessionsLocked()
	if err != nil {
		return err
	}
	next := file.Sessions[:0]
	for _, session := range file.Sessions {
		if session.UserID != userID {
			next = append(next, session)
		}
	}
	file.Sessions = next
	return s.saveSessionsLocked(file)
}
