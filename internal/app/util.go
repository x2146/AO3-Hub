package app

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultUpdateManifestURL       = "https://github.com/x2146/AO3-Hub/releases/latest/download/manifest.json"
	DefaultDevUpdateManifestURL    = "https://github.com/x2146/AO3-Hub/releases/download/dev/manifest.json"
	LLMAPITypeOpenAICompatible     = "openai-compatible"
	LLMAPITypeClaudeMessages       = "claude-messages"
	DefaultOpenAICompatibleBaseURL = "https://api.deepseek.com/v1"
	DefaultOpenAICompatibleModel   = "deepseek-chat"
	DefaultClaudeMessagesBaseURL   = "https://api.anthropic.com/v1"
	DefaultClaudeMessagesModel     = "claude-sonnet-4-5"
)

var (
	Version = "dev-local"
	BuiltAt = ""
)

func init() {
	Version = envOr("AO3HUB_VERSION", Version)
	BuiltAt = envOr("AO3HUB_BUILT_AT", BuiltAt)
	if BuiltAt == "" {
		BuiltAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
}

func versionLabel(version string) string {
	v := strings.TrimSpace(version)
	if v == "" {
		return "dev-local"
	}
	if strings.HasPrefix(v, "v") || strings.HasPrefix(v, "dev-") {
		return v
	}
	if regexp.MustCompile(`^\d+\.\d+\.\d+(?:[-+].*)?$`).MatchString(v) {
		return "v" + v
	}
	return v
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func dataDir() (string, error) {
	if raw := strings.TrimSpace(os.Getenv("AO3HUB_DATA_DIR")); raw != "" {
		return filepath.Abs(raw)
	}
	rootData := filepath.Join(".", "data")
	if _, err := os.Stat(rootData); err == nil {
		return filepath.Abs(rootData)
	}
	legacyServerData := filepath.Join(".", "server", "data")
	if _, err := os.Stat(legacyServerData); err == nil {
		return filepath.Abs(legacyServerData)
	}
	return filepath.Abs(rootData)
}

func resolveHost(configHost string) string {
	if raw := strings.TrimSpace(os.Getenv("HOST")); raw != "" {
		return raw
	}
	if strings.TrimSpace(configHost) == "" {
		return "0.0.0.0"
	}
	return configHost
}

func resolvePort(configPort int) (int, error) {
	raw := strings.TrimSpace(os.Getenv("PORT"))
	if raw == "" {
		if configPort <= 0 {
			return 3000, nil
		}
		return configPort, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("invalid PORT: %s", raw)
	}
	return port, nil
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func randomHex(bytesLen int) (string, error) {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func randomUserID() (string, error) {
	token, err := randomHex(8)
	if err != nil {
		return "", err
	}
	return "u_" + token, nil
}

func randomStoryID() string {
	token, err := randomHex(5)
	if err != nil {
		return "u" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return "u" + token + strconv.FormatInt(time.Now().UnixMilli(), 36)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("content-type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func decodeJSON(r *http.Request, dst any) error {
	defer r.Body.Close()
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return err
	}
	return nil
}

func publicUser(u UserRecord) PublicUser {
	return PublicUser{
		ID:        u.ID,
		Username:  u.Username,
		Role:      u.Role,
		CreatedAt: u.CreatedAt,
		UpdatedAt: u.UpdatedAt,
	}
}

func normalizeTags(t Tags) Tags {
	if t.Fandom == nil {
		t.Fandom = []string{}
	}
	if t.Relationship == nil {
		t.Relationship = []string{}
	}
	if t.Character == nil {
		t.Character = []string{}
	}
	if t.Additional == nil {
		t.Additional = []string{}
	}
	if t.Warnings == nil {
		t.Warnings = []string{}
	}
	if t.Categories == nil {
		t.Categories = []string{}
	}
	return t
}

func normalizeMeta(m Meta) Meta {
	m.Tags = normalizeTags(m.Tags)
	if m.Language == "" {
		m.Language = "en"
	}
	if m.ChapterCount < 0 {
		m.ChapterCount = 0
	}
	if m.WordCount < 0 {
		m.WordCount = 0
	}
	return m
}

func normalizeProgress(p Progress) Progress {
	if p.Errors == nil {
		p.Errors = []ProgressError{}
	}
	return p
}

func defaultConfig() Config {
	return Config{
		Server: ServerConfig{
			Host: "0.0.0.0",
			Port: 3000,
		},
		Auth: AuthConfig{
			SessionTTLDays: 30,
		},
		Stream: StreamConfig{
			HeartbeatMS: 15000,
		},
		Import: ImportConfig{
			MinHTMLLength: 100,
		},
		UI: UIConfig{
			LibraryRefetchIntervalMS: 3000,
		},
		LLM: defaultLLMConfig(LLMAPITypeOpenAICompatible),
		AO3: AO3Config{
			Cookie:    "",
			UserAgent: "Mozilla/5.0 (Macintosh; Intel Mac OS X 14_5) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.0 Safari/605.1.15",
		},
		Reader: ReaderConfig{
			DefaultMeasure: 780,
			DefaultFont:    17,
			DefaultZHScale: 0.96,
		},
		Update: UpdateConfig{
			ManifestURL:    DefaultUpdateManifestURL,
			Channel:        "stable",
			AutoCheck:      false,
			RestartDelayMS: 600,
		},
	}
}

func defaultLLMConfig(apiType string) LLMConfig {
	normalized := normalizeLLMAPIType(apiType)
	cfg := LLMConfig{
		APIType:             LLMAPITypeOpenAICompatible,
		BaseURL:             DefaultOpenAICompatibleBaseURL,
		APIKey:              "",
		Model:               DefaultOpenAICompatibleModel,
		Temperature:         0.3,
		Concurrency:         3,
		BlocksPerRequest:    8,
		MaxTokensPerRequest: 3500,
	}
	if normalized == LLMAPITypeClaudeMessages {
		cfg.APIType = LLMAPITypeClaudeMessages
		cfg.BaseURL = DefaultClaudeMessagesBaseURL
		cfg.Model = DefaultClaudeMessagesModel
	}
	return cfg
}

func normalizeConfig(c Config) Config {
	d := defaultConfig()
	if strings.TrimSpace(c.Server.Host) == "" {
		c.Server.Host = d.Server.Host
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		c.Server.Port = d.Server.Port
	}
	if c.Auth.SessionTTLDays <= 0 {
		c.Auth.SessionTTLDays = d.Auth.SessionTTLDays
	}
	if c.Stream.HeartbeatMS <= 0 {
		c.Stream.HeartbeatMS = d.Stream.HeartbeatMS
	}
	if c.Import.MinHTMLLength < 0 {
		c.Import.MinHTMLLength = d.Import.MinHTMLLength
	}
	if c.UI.LibraryRefetchIntervalMS <= 0 {
		c.UI.LibraryRefetchIntervalMS = d.UI.LibraryRefetchIntervalMS
	}
	c.LLM.APIType = normalizeLLMAPIType(c.LLM.APIType)
	llmDefaults := defaultLLMConfig(c.LLM.APIType)
	if strings.TrimSpace(c.LLM.BaseURL) == "" {
		c.LLM.BaseURL = llmDefaults.BaseURL
	}
	if strings.TrimSpace(c.LLM.Model) == "" {
		c.LLM.Model = llmDefaults.Model
	}
	if c.LLM.Concurrency <= 0 {
		c.LLM.Concurrency = llmDefaults.Concurrency
	}
	if c.LLM.BlocksPerRequest <= 0 {
		c.LLM.BlocksPerRequest = llmDefaults.BlocksPerRequest
	}
	if c.LLM.MaxTokensPerRequest <= 0 {
		c.LLM.MaxTokensPerRequest = llmDefaults.MaxTokensPerRequest
	}
	if strings.TrimSpace(c.AO3.UserAgent) == "" {
		c.AO3.UserAgent = d.AO3.UserAgent
	}
	if c.Reader.DefaultMeasure <= 0 {
		c.Reader.DefaultMeasure = d.Reader.DefaultMeasure
	}
	if c.Reader.DefaultFont <= 0 {
		c.Reader.DefaultFont = d.Reader.DefaultFont
	}
	if c.Reader.DefaultZHScale <= 0 {
		c.Reader.DefaultZHScale = d.Reader.DefaultZHScale
	}
	if strings.TrimSpace(c.Update.ManifestURL) == "" {
		c.Update.ManifestURL = d.Update.ManifestURL
	}
	if strings.TrimSpace(c.Update.Channel) == "" {
		c.Update.Channel = d.Update.Channel
	}
	if c.Update.RestartDelayMS < 0 {
		c.Update.RestartDelayMS = d.Update.RestartDelayMS
	}
	return c
}

func validateConfig(c Config) error {
	if strings.TrimSpace(c.Server.Host) == "" {
		return errors.New("server.host is required")
	}
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return errors.New("server.port out of range")
	}
	if c.Auth.SessionTTLDays <= 0 {
		return errors.New("auth.sessionTtlDays must be positive")
	}
	if c.Stream.HeartbeatMS <= 0 {
		return errors.New("stream.heartbeatMs must be positive")
	}
	if c.Import.MinHTMLLength < 0 {
		return errors.New("import.minHtmlLength must be nonnegative")
	}
	if c.UI.LibraryRefetchIntervalMS <= 0 {
		return errors.New("ui.libraryRefetchIntervalMs must be positive")
	}
	if !validLLMAPIType(normalizeLLMAPIType(c.LLM.APIType)) {
		return errors.New("llm.apiType is invalid")
	}
	if strings.TrimSpace(c.LLM.BaseURL) == "" {
		return errors.New("llm.baseURL is required")
	}
	if strings.TrimSpace(c.LLM.Model) == "" {
		return errors.New("llm.model is required")
	}
	if c.LLM.Concurrency <= 0 {
		return errors.New("llm.concurrency must be positive")
	}
	if c.LLM.BlocksPerRequest <= 0 {
		return errors.New("llm.blocksPerRequest must be positive")
	}
	if c.LLM.MaxTokensPerRequest <= 0 {
		return errors.New("llm.maxTokensPerRequest must be positive")
	}
	if c.Reader.DefaultMeasure <= 0 {
		return errors.New("reader.defaultMeasure must be positive")
	}
	if c.Reader.DefaultFont <= 0 {
		return errors.New("reader.defaultFont must be positive")
	}
	if c.Reader.DefaultZHScale <= 0 {
		return errors.New("reader.defaultZhScale must be positive")
	}
	if strings.TrimSpace(c.Update.ManifestURL) == "" {
		return errors.New("update.manifestURL is required")
	}
	if strings.TrimSpace(c.Update.Channel) == "" {
		return errors.New("update.channel is required")
	}
	if c.Update.RestartDelayMS < 0 {
		return errors.New("update.restartDelayMs must be nonnegative")
	}
	return nil
}

func normalizeLLMAPIType(apiType string) string {
	switch strings.ToLower(strings.TrimSpace(apiType)) {
	case "", "openai", "openai-compatible", "chat-completions":
		return LLMAPITypeOpenAICompatible
	case "anthropic", "claude", "claude-messages", "anthropic-messages":
		return LLMAPITypeClaudeMessages
	default:
		return strings.ToLower(strings.TrimSpace(apiType))
	}
}

func normalizeLLMProviderDefaults(next *LLMConfig, previous LLMConfig, patch map[string]json.RawMessage) {
	previousType := normalizeLLMAPIType(previous.APIType)
	nextType := normalizeLLMAPIType(next.APIType)
	if previousType == nextType {
		return
	}
	nextDefaults := defaultLLMConfig(nextType)
	previousDefaults := defaultLLMConfig(previousType)
	_, hasBaseURLPatch := patch["baseURL"]
	keptPreviousBaseURLDefault := strings.TrimSpace(previous.BaseURL) == previousDefaults.BaseURL &&
		strings.TrimSpace(next.BaseURL) == previousDefaults.BaseURL
	if (!hasBaseURLPatch && strings.TrimSpace(previous.BaseURL) == previousDefaults.BaseURL) || keptPreviousBaseURLDefault {
		next.BaseURL = nextDefaults.BaseURL
	}
	_, hasModelPatch := patch["model"]
	keptPreviousModelDefault := strings.TrimSpace(previous.Model) == previousDefaults.Model &&
		strings.TrimSpace(next.Model) == previousDefaults.Model
	if (!hasModelPatch && strings.TrimSpace(previous.Model) == previousDefaults.Model) || keptPreviousModelDefault {
		next.Model = nextDefaults.Model
	}
}

func validLLMAPIType(apiType string) bool {
	return apiType == LLMAPITypeOpenAICompatible || apiType == LLMAPITypeClaudeMessages
}

func maskSecret(key string) string {
	if key == "" {
		return ""
	}
	runes := []rune(key)
	if len(runes) <= 8 {
		return strings.Repeat("*", len(runes))
	}
	return string(runes[:4]) + "…" + string(runes[len(runes)-4:])
}

var usernameRE = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,32}$`)

func validateUsernamePassword(username, password string) error {
	if !usernameRE.MatchString(username) {
		return errors.New("参数无效")
	}
	if len(password) < 6 || len(password) > 200 {
		return errors.New("参数无效")
	}
	return nil
}

func validateRole(role Role) bool {
	return role == RoleAdmin || role == RoleUser
}

func platformName() string {
	return runtime.GOOS
}

func archName() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x64"
	default:
		return runtime.GOARCH
	}
}
