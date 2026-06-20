package app

type StoryStatus string

const (
	StatusQueued      StoryStatus = "queued"
	StatusFetching    StoryStatus = "fetching"
	StatusParsing     StoryStatus = "parsing"
	StatusAnalyzing   StoryStatus = "analyzing"
	StatusTranslating StoryStatus = "translating"
	StatusReady       StoryStatus = "ready"
	StatusError       StoryStatus = "error"
)

type TranslationMode string

const (
	TranslationModeNormal  TranslationMode = "normal"
	TranslationModeRefined TranslationMode = "refined"
)

type IndexEntry struct {
	ID           string      `json:"id"`
	Title        string      `json:"title"`
	ChineseTitle string      `json:"chineseTitle,omitempty"`
	Author       string      `json:"author"`
	ChapterCount int         `json:"chapterCount"`
	WordCount    int         `json:"wordCount"`
	Status       StoryStatus `json:"status"`
	AddedAt      string      `json:"addedAt"`
	UpdatedAt    string      `json:"updatedAt"`
}

type IndexFile struct {
	Stories []IndexEntry `json:"stories"`
}

type StoryListItem struct {
	IndexEntry
	Progress *Progress `json:"progress,omitempty"`
}

type StoryList struct {
	Stories []StoryListItem `json:"stories"`
}

type Tags struct {
	Fandom       []string `json:"fandom"`
	Relationship []string `json:"relationship"`
	Character    []string `json:"character"`
	Additional   []string `json:"additional"`
	Rating       string   `json:"rating,omitempty"`
	Warnings     []string `json:"warnings"`
	Categories   []string `json:"categories"`
}

type Meta struct {
	ID              string          `json:"id"`
	URL             string          `json:"url"`
	DownloadURL     string          `json:"downloadUrl,omitempty"`
	Title           string          `json:"title"`
	ChineseTitle    string          `json:"chineseTitle,omitempty"`
	Author          string          `json:"author"`
	AuthorURL       string          `json:"authorUrl,omitempty"`
	Summary         string          `json:"summary,omitempty"`
	Notes           string          `json:"notes,omitempty"`
	Endnotes        string          `json:"endnotes,omitempty"`
	Tags            Tags            `json:"tags"`
	Language        string          `json:"language"`
	PublishedAt     string          `json:"publishedAt,omitempty"`
	UpdatedAt       string          `json:"updatedAt,omitempty"`
	WordCount       int             `json:"wordCount"`
	ChapterCount    int             `json:"chapterCount"`
	TranslationMode TranslationMode `json:"translationMode,omitempty"`
}

type BlockType string

const (
	BlockP          BlockType = "p"
	BlockH2         BlockType = "h2"
	BlockH3         BlockType = "h3"
	BlockBlockquote BlockType = "blockquote"
	BlockHR         BlockType = "hr"
	BlockPre        BlockType = "pre"
	BlockUL         BlockType = "ul"
	BlockOL         BlockType = "ol"
)

type BlockStatus string

const (
	BlockPending BlockStatus = "pending"
	BlockDone    BlockStatus = "done"
	BlockError   BlockStatus = "error"
)

type Block struct {
	ID     string      `json:"id"`
	Type   BlockType   `json:"type"`
	HTML   string      `json:"html"`
	Status BlockStatus `json:"status,omitempty"`
	Error  string      `json:"error,omitempty"`
}

type Chapter struct {
	Index  int     `json:"index"`
	Title  string  `json:"title,omitempty"`
	Blocks []Block `json:"blocks"`
}

type ChapterFile struct {
	Chapters []Chapter `json:"chapters"`
}

type ProgressPhase string

const (
	PhaseQueued      ProgressPhase = "queued"
	PhaseFetching    ProgressPhase = "fetching"
	PhaseParsing     ProgressPhase = "parsing"
	PhaseAnalyzing   ProgressPhase = "analyzing"
	PhaseTranslating ProgressPhase = "translating"
	PhaseReady       ProgressPhase = "ready"
	PhaseError       ProgressPhase = "error"
)

type ProgressError struct {
	ChapterIndex int    `json:"chapterIndex"`
	BlockID      string `json:"blockId"`
	Message      string `json:"message"`
	At           string `json:"at"`
}

type Progress struct {
	Phase          ProgressPhase   `json:"phase"`
	TotalBlocks    int             `json:"totalBlocks"`
	DoneBlocks     int             `json:"doneBlocks"`
	ErrorBlocks    int             `json:"errorBlocks"`
	InflightBlocks int             `json:"inflightBlocks"`
	CurrentChapter *int            `json:"currentChapter,omitempty"`
	StartedAt      string          `json:"startedAt"`
	FinishedAt     string          `json:"finishedAt,omitempty"`
	Message        string          `json:"message,omitempty"`
	Errors         []ProgressError `json:"errors"`
}

type LLMConfig struct {
	APIType                string          `json:"apiType"`
	BaseURL                string          `json:"baseURL"`
	APIKey                 string          `json:"apiKey"`
	Model                  string          `json:"model"`
	Temperature            float64         `json:"temperature"`
	Concurrency            int             `json:"concurrency"`
	BlocksPerRequest       int             `json:"blocksPerRequest"`
	MaxTokensPerRequest    int             `json:"maxTokensPerRequest"`
	MaxAutoRetries         int             `json:"maxAutoRetries"`
	Mode                   TranslationMode `json:"mode"`
	AnalysisMaxInputTokens int             `json:"analysisMaxInputTokens"`
	Stream                 bool            `json:"stream"`
}

type AO3Config struct {
	Cookie    string `json:"cookie"`
	UserAgent string `json:"userAgent"`
}

type ReaderConfig struct {
	DefaultMeasure int     `json:"defaultMeasure"`
	DefaultFont    int     `json:"defaultFont"`
	DefaultZHScale float64 `json:"defaultZhScale"`
}

type ServerConfig struct {
	Host string `json:"host"`
	Port int    `json:"port"`
}

type AuthConfig struct {
	SessionTTLDays int `json:"sessionTtlDays"`
}

type StreamConfig struct {
	HeartbeatMS int `json:"heartbeatMs"`
}

type ImportConfig struct {
	MinHTMLLength int `json:"minHtmlLength"`
}

type UIConfig struct {
	LibraryRefetchIntervalMS int `json:"libraryRefetchIntervalMs"`
}

type UpdateConfig struct {
	ManifestURL    string `json:"manifestURL"`
	Channel        string `json:"channel"`
	AutoCheck      bool   `json:"autoCheck"`
	RestartDelayMS int    `json:"restartDelayMs"`
}

type Config struct {
	Server ServerConfig `json:"server"`
	Auth   AuthConfig   `json:"auth"`
	Stream StreamConfig `json:"stream"`
	Import ImportConfig `json:"import"`
	UI     UIConfig     `json:"ui"`
	LLM    LLMConfig    `json:"llm"`
	AO3    AO3Config    `json:"ao3"`
	Reader ReaderConfig `json:"reader"`
	Update UpdateConfig `json:"update"`
}

type ManifestAsset struct {
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256,omitempty"`
	Size     int64  `json:"size,omitempty"`
}

type Manifest struct {
	Version     string          `json:"version"`
	Channel     string          `json:"channel,omitempty"`
	Notes       string          `json:"notes,omitempty"`
	PublishedAt string          `json:"publishedAt,omitempty"`
	Assets      []ManifestAsset `json:"assets"`
}

type LatestVersion struct {
	Version      string `json:"version"`
	Channel      string `json:"channel,omitempty"`
	Notes        string `json:"notes,omitempty"`
	PublishedAt  string `json:"publishedAt,omitempty"`
	HasUpdate    bool   `json:"hasUpdate"`
	Strategy     string `json:"strategy,omitempty"`
	UpdateReason string `json:"updateReason,omitempty"`
	DownloadURL  string `json:"downloadUrl,omitempty"`
}

type VersionInfo struct {
	Current  string         `json:"current"`
	Platform string         `json:"platform"`
	Arch     string         `json:"arch"`
	BuiltAt  string         `json:"builtAt,omitempty"`
	Latest   *LatestVersion `json:"latest,omitempty"`
}

type ChapterPair struct {
	ID     string      `json:"id"`
	Type   BlockType   `json:"type"`
	En     string      `json:"en"`
	ZH     string      `json:"zh,omitempty"`
	Status BlockStatus `json:"status"`
	Error  string      `json:"error,omitempty"`
}

type ChapterView struct {
	Meta     Meta     `json:"meta"`
	Progress Progress `json:"progress"`
	Chapter  struct {
		Index   int           `json:"index"`
		TitleEn string        `json:"titleEn,omitempty"`
		TitleZH string        `json:"titleZh,omitempty"`
		Pairs   []ChapterPair `json:"pairs"`
	} `json:"chapter"`
	Nav struct {
		Prev  *int `json:"prev,omitempty"`
		Next  *int `json:"next,omitempty"`
		Total int  `json:"total"`
	} `json:"nav"`
}

type Role string

const (
	RoleAdmin Role = "admin"
	RoleUser  Role = "user"
)

type PublicUser struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	Role      Role   `json:"role"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

type UserRecord struct {
	ID           string `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"passwordHash"`
	Role         Role   `json:"role"`
	CreatedAt    string `json:"createdAt"`
	UpdatedAt    string `json:"updatedAt"`
}

type UsersFile struct {
	Users []UserRecord `json:"users"`
}

type SessionRecord struct {
	Token      string `json:"token"`
	UserID     string `json:"userId"`
	CreatedAt  string `json:"createdAt"`
	LastUsedAt string `json:"lastUsedAt"`
	ExpiresAt  string `json:"expiresAt"`
}

type SessionsFile struct {
	Sessions []SessionRecord `json:"sessions"`
}

type AuthMe struct {
	User       *PublicUser `json:"user"`
	NeedsSetup bool        `json:"needsSetup"`
}

type Character struct {
	Name string `json:"name"`
	Zh   string `json:"zh,omitempty"`
	Role string `json:"role,omitempty"`
}

type ChapterSummary struct {
	Index   int    `json:"index"`
	Title   string `json:"title,omitempty"`
	Summary string `json:"summary"`
}

type TranslationContext struct {
	Summary          string            `json:"summary,omitempty"`
	Tone             string            `json:"tone,omitempty"`
	Ships            []string          `json:"ships"`
	Characters       []Character       `json:"characters"`
	Glossary         map[string]string `json:"glossary"`
	ChapterSummaries []ChapterSummary  `json:"chapterSummaries"`
	GeneratedAt      string            `json:"generatedAt,omitempty"`
	ChapterCount     int               `json:"chapterCount,omitempty"`
}

type StreamEvent struct {
	Type           string        `json:"type"`
	DoneBlocks     int           `json:"doneBlocks,omitempty"`
	TotalBlocks    int           `json:"totalBlocks,omitempty"`
	ErrorBlocks    int           `json:"errorBlocks,omitempty"`
	InflightBlocks int           `json:"inflightBlocks,omitempty"`
	Phase          ProgressPhase `json:"phase,omitempty"`
	ChapterIndex   int           `json:"chapterIndex,omitempty"`
	BlockID        string        `json:"blockId,omitempty"`
	Message        string        `json:"message,omitempty"`
}

type LLMCallStage string

const (
	StageAnalysisChapter LLMCallStage = "analysis-chapter"
	StageAnalysisMerge   LLMCallStage = "analysis-merge"
	StageAnalysisFull    LLMCallStage = "analysis-full"
	StageTranslateBatch  LLMCallStage = "translate-batch"
)

type LLMCallStatus string

const (
	LLMCallSuccess LLMCallStatus = "success"
	LLMCallError   LLMCallStatus = "error"
)

type LLMCallEvent struct {
	ID               string        `json:"id"`
	Stage            LLMCallStage  `json:"stage"`
	Status           LLMCallStatus `json:"status"`
	Model            string        `json:"model,omitempty"`
	StartedAt        string        `json:"startedAt"`
	DurationMS       int64         `json:"durationMs"`
	PromptTokens     int           `json:"promptTokens"`
	CompletionTokens int           `json:"completionTokens"`
	TotalTokens      int           `json:"totalTokens"`
	Attempt          int           `json:"attempt"`
	ChapterIndex     *int          `json:"chapterIndex,omitempty"`
	BlockIDs         []string      `json:"blockIds,omitempty"`
	ErrorMessage     string        `json:"errorMessage,omitempty"`
	ErrorStatus      int           `json:"errorStatus,omitempty"`
}

type StageStats struct {
	Calls            int   `json:"calls"`
	Successes        int   `json:"successes"`
	Failures         int   `json:"failures"`
	Retries          int   `json:"retries"`
	PromptTokens     int   `json:"promptTokens"`
	CompletionTokens int   `json:"completionTokens"`
	TotalTokens      int   `json:"totalTokens"`
	DurationMS       int64 `json:"durationMs"`
}

type TranslationStats struct {
	Total      StageStats                  `json:"total"`
	ByStage    map[LLMCallStage]StageStats `json:"byStage"`
	StartedAt  string                      `json:"startedAt,omitempty"`
	LastCallAt string                      `json:"lastCallAt,omitempty"`
}

type RequestSample struct {
	Stage           LLMCallStage `json:"stage"`
	CapturedAt      string       `json:"capturedAt"`
	Model           string       `json:"model,omitempty"`
	SystemPrompt    string       `json:"systemPrompt"`
	UserPayload     string       `json:"userPayload"`
	ResponsePreview string       `json:"responsePreview,omitempty"`
	ChapterIndex    *int         `json:"chapterIndex,omitempty"`
	BlockIDs        []string     `json:"blockIds,omitempty"`
}

type StatsFile struct {
	Stats   TranslationStats              `json:"stats"`
	Events  []LLMCallEvent                `json:"events"`
	Samples map[LLMCallStage]RequestSample `json:"samples"`
}

type TranslationStatusView struct {
	Stats   TranslationStats               `json:"stats"`
	Events  []LLMCallEvent                 `json:"events"`
	Samples map[LLMCallStage]RequestSample `json:"samples"`
	Context *TranslationContext            `json:"context,omitempty"`
	Mode    TranslationMode                `json:"mode"`
}
