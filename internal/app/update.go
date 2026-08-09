package app

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	updateManifestTimeout         = 30 * time.Second
	updateDownloadTimeout         = 30 * time.Minute
	updateAutoCheckInterval       = 15 * time.Minute
	maxUpdateManifestBytes        = 1 << 20
	maxUpdateSignatureBytes       = 4 << 10
	maxUpdateAssetBytes     int64 = 512 << 20
)

var (
	stableVersionRE  = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$`)
	devVersionRE     = regexp.MustCompile(`^dev-(\d+)-\d{8}-([A-Za-z0-9]+)(?:[-+].*)?$`)
	updateHTTPClient = &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("too many update redirects")
			}
			return validateUpdateURL(req.URL.String())
		},
	}
)

// UpdateSigningPublicKey is injected into release binaries with -ldflags -X.
// An empty value deliberately disables OTA rather than falling back to unsigned updates.
var UpdateSigningPublicKey string

type updateCheck struct {
	HasUpdate bool
	Strategy  string
	Reason    string
}

type updateManifestCache struct {
	mu          sync.Mutex
	manifest    Manifest
	manifestKey string
	hasManifest bool
	checking    bool
	attemptKey  string
	attemptedAt time.Time
}

func normalizeVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}

func sameVersion(a, b string) bool {
	return normalizeVersion(a) == normalizeVersion(b)
}

func isDevVersion(version string) bool {
	return strings.HasPrefix(normalizeVersion(version), "dev-")
}

func isLocalDevVersion(version string) bool {
	switch normalizeVersion(version) {
	case "", "dev", "dev-local":
		return true
	default:
		return false
	}
}

func parseStableVersion(version string) ([3]int, bool) {
	var out [3]int
	match := stableVersionRE.FindStringSubmatch(strings.TrimSpace(version))
	if len(match) != 4 {
		return out, false
	}
	for i := 0; i < 3; i++ {
		n, err := strconv.Atoi(match[i+1])
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func compareStableVersions(a, b string) (int, bool) {
	av, aOK := parseStableVersion(a)
	bv, bOK := parseStableVersion(b)
	if !aOK || !bOK {
		return 0, false
	}
	for i := 0; i < 3; i++ {
		if av[i] != bv[i] {
			return av[i] - bv[i], true
		}
	}
	return 0, true
}

func parseDevVersion(version string) (int64, string, bool) {
	match := devVersionRE.FindStringSubmatch(normalizeVersion(version))
	if len(match) != 3 {
		return 0, "", false
	}
	n, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		return 0, "", false
	}
	return n, match[2], true
}

func normalizeUpdateChannel(channel string) string {
	if strings.ToLower(strings.TrimSpace(channel)) == "dev" {
		return "dev"
	}
	return "stable"
}

func checkForUpdate(channel, current, remote string) updateCheck {
	switch normalizeUpdateChannel(channel) {
	case "dev":
		return checkDevUpdate(current, remote)
	default:
		return checkStableUpdate(current, remote)
	}
}

func checkStableUpdate(current, remote string) updateCheck {
	check := updateCheck{Strategy: "stable-semver"}
	if sameVersion(remote, current) {
		check.Reason = "版本相同"
		return check
	}
	if _, ok := parseStableVersion(remote); !ok {
		check.Reason = "stable channel 需要远端版本是 semver"
		return check
	}
	if isLocalDevVersion(current) || isDevVersion(current) {
		check.HasUpdate = true
		check.Reason = "当前为 dev 构建，允许切换到 stable"
		return check
	}
	cmp, ok := compareStableVersions(remote, current)
	if !ok {
		check.Reason = "当前版本不是可比较的 semver"
		return check
	}
	if cmp > 0 {
		check.HasUpdate = true
		check.Reason = "远端 stable 版本更新"
		return check
	}
	check.Reason = "当前 stable 版本已是最新"
	return check
}

func checkDevUpdate(current, remote string) updateCheck {
	check := updateCheck{Strategy: "dev-run"}
	remoteRun, remoteSHA, remoteOK := parseDevVersion(remote)
	if !remoteOK {
		check.Reason = "dev channel 需要远端版本是 dev CI tag"
		return check
	}
	if isLocalDevVersion(current) {
		check.HasUpdate = true
		check.Reason = "本地 dev 版本未注入 CI run，允许升级"
		return check
	}
	localRun, localSHA, localOK := parseDevVersion(current)
	if !localOK {
		check.Reason = "当前版本不是 dev CI tag，避免自动切换或回退"
		return check
	}
	if remoteSHA != "" && localSHA != "" && remoteSHA == localSHA {
		check.Reason = "commit SHA 相同"
		return check
	}
	if remoteRun > localRun {
		check.HasUpdate = true
		check.Reason = "远端 dev run number 更新"
		return check
	}
	check.Reason = "当前 dev run number 已是最新"
	return check
}

func platformMatch(asset ManifestAsset) bool {
	return asset.Platform == platformName() && asset.Arch == archName()
}

func manifestURLForChannel(channel string) string {
	if strings.ToLower(strings.TrimSpace(channel)) == "dev" {
		return DefaultDevUpdateManifestURL
	}
	return DefaultUpdateManifestURL
}

func resolveManifestURL(cfg Config) string {
	url := strings.TrimSpace(cfg.Update.ManifestURL)
	if url == "" || url == DefaultUpdateManifestURL || url == DefaultDevUpdateManifestURL {
		return manifestURLForChannel(cfg.Update.Channel)
	}
	return url
}

func fetchManifest(cfg Config) (*Manifest, string) {
	ctx, cancel := context.WithTimeout(context.Background(), updateManifestTimeout)
	defer cancel()
	manifest, err := fetchManifestContext(ctx, cfg)
	if err != nil {
		return nil, err.Error()
	}
	return manifest, ""
}

func fetchManifestContext(ctx context.Context, cfg Config) (*Manifest, error) {
	if _, err := embeddedUpdateSigningPublicKey(); err != nil {
		return nil, err
	}
	manifestURL := resolveManifestURL(cfg)
	if manifestURL == "" {
		return nil, errors.New("未配置 manifest URL")
	}
	body, err := fetchUpdateBytes(ctx, manifestURL, maxUpdateManifestBytes)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	signatureURL, err := manifestSignatureURL(manifestURL)
	if err != nil {
		return nil, err
	}
	signature, err := fetchUpdateBytes(ctx, signatureURL, maxUpdateSignatureBytes)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest signature: %w", err)
	}
	if err := verifyManifestSignature(body, signature); err != nil {
		return nil, err
	}

	var manifest Manifest
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, errors.New("manifest schema 校验失败")
	}
	if err := validateManifest(manifest); err != nil {
		return nil, fmt.Errorf("manifest schema 校验失败: %w", err)
	}
	return &manifest, nil
}

func fetchUpdateBytes(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	if err := validateUpdateURL(rawURL); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("accept-encoding", "identity")
	req.Header.Set("cache-control", "no-cache")
	res, err := updateHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Errorf("request failed: %d", res.StatusCode)
	}
	if res.ContentLength > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return body, nil
}

func manifestSignatureURL(rawURL string) (string, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("invalid manifest URL: %w", err)
	}
	parsed.Path += ".sig"
	parsed.RawPath = ""
	return parsed.String(), nil
}

func verifyManifestSignature(message, signature []byte) error {
	publicKey, err := embeddedUpdateSigningPublicKey()
	if err != nil {
		return err
	}
	rawSignature, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(signature)))
	if err != nil {
		return fmt.Errorf("decode manifest signature: %w", err)
	}
	if !ed25519.Verify(publicKey, message, rawSignature) {
		return errors.New("manifest signature verification failed")
	}
	return nil
}

func embeddedUpdateSigningPublicKey() (ed25519.PublicKey, error) {
	publicKey, err := hex.DecodeString(strings.TrimSpace(UpdateSigningPublicKey))
	if err != nil || len(publicKey) != ed25519.PublicKeySize {
		return nil, errors.New("invalid embedded update signing public key")
	}
	return ed25519.PublicKey(publicKey), nil
}

func validateManifest(manifest Manifest) error {
	if strings.TrimSpace(manifest.Version) == "" || len(manifest.Assets) == 0 {
		return errors.New("version and assets are required")
	}
	seen := make(map[string]struct{}, len(manifest.Assets))
	for _, asset := range manifest.Assets {
		if strings.TrimSpace(asset.Platform) == "" || strings.TrimSpace(asset.Arch) == "" {
			return errors.New("asset platform and arch are required")
		}
		key := asset.Platform + "/" + asset.Arch
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate asset %s", key)
		}
		seen[key] = struct{}{}
		if err := validateManifestAsset(asset); err != nil {
			return fmt.Errorf("asset %s: %w", key, err)
		}
	}
	return nil
}

func validateManifestAsset(asset ManifestAsset) error {
	if err := validateUpdateURL(asset.URL); err != nil {
		return err
	}
	if asset.Size <= 0 || asset.Size > maxUpdateAssetBytes {
		return fmt.Errorf("size must be between 1 and %d bytes", maxUpdateAssetBytes)
	}
	digest, err := hex.DecodeString(strings.TrimSpace(asset.SHA256))
	if err != nil || len(digest) != sha256.Size {
		return errors.New("sha256 must be a 32-byte hex digest")
	}
	return nil
}

func validateUpdateURL(rawURL string) error {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return errors.New("invalid update URL")
	}
	if parsed.User != nil || parsed.Fragment != "" {
		return errors.New("update URL must not contain credentials or fragments")
	}
	if strings.EqualFold(parsed.Scheme, "https") {
		return nil
	}
	return errors.New("update URL must use HTTPS")
}

func (a *App) FetchManifest() (*Manifest, string) {
	cfg, err := a.store.LoadConfig()
	if err != nil {
		return nil, err.Error()
	}
	return fetchManifest(cfg)
}

func (a *App) VersionInfo() VersionInfo {
	base := VersionInfo{
		Current:  Version,
		Platform: platformName(),
		Arch:     archName(),
		BuiltAt:  BuiltAt,
	}
	cfg, err := a.store.LoadConfig()
	if err != nil {
		return base
	}
	manifest := a.cachedUpdateManifest(cfg)
	a.maybeAutoCheckUpdates(cfg)
	return versionInfoForManifest(base, cfg, manifest)
}

func (a *App) CheckUpdate(parent context.Context) (VersionInfo, error) {
	base := VersionInfo{
		Current:  Version,
		Platform: platformName(),
		Arch:     archName(),
		BuiltAt:  BuiltAt,
	}
	cfg, err := a.store.LoadConfig()
	if err != nil {
		return base, err
	}
	ctx, cancel := context.WithTimeout(parent, updateManifestTimeout)
	defer cancel()
	manifest, err := fetchManifestContext(ctx, cfg)
	if err != nil {
		return base, err
	}
	a.cacheUpdateManifest(cfg, *manifest)
	return versionInfoForManifest(base, cfg, manifest), nil
}

func versionInfoForManifest(base VersionInfo, cfg Config, manifest *Manifest) VersionInfo {
	if manifest == nil {
		return base
	}
	var asset *ManifestAsset
	for i := range manifest.Assets {
		if platformMatch(manifest.Assets[i]) {
			asset = &manifest.Assets[i]
			break
		}
	}
	check := checkForUpdate(cfg.Update.Channel, Version, manifest.Version)
	latest := &LatestVersion{
		Version:      manifest.Version,
		Channel:      manifest.Channel,
		Notes:        manifest.Notes,
		PublishedAt:  manifest.PublishedAt,
		HasUpdate:    check.HasUpdate,
		Strategy:     check.Strategy,
		UpdateReason: check.Reason,
	}
	if asset != nil {
		latest.DownloadURL = asset.URL
	}
	base.Latest = latest
	return base
}

func updateManifestCacheKey(cfg Config) string {
	return normalizeUpdateChannel(cfg.Update.Channel) + "\x00" + resolveManifestURL(cfg)
}

func (a *App) cachedUpdateManifest(cfg Config) *Manifest {
	key := updateManifestCacheKey(cfg)
	a.updateCache.mu.Lock()
	defer a.updateCache.mu.Unlock()
	if !a.updateCache.hasManifest || a.updateCache.manifestKey != key {
		return nil
	}
	manifest := a.updateCache.manifest
	manifest.Assets = append([]ManifestAsset(nil), manifest.Assets...)
	return &manifest
}

func (a *App) cacheUpdateManifest(cfg Config, manifest Manifest) {
	a.updateCache.mu.Lock()
	defer a.updateCache.mu.Unlock()
	key := updateManifestCacheKey(cfg)
	manifest.Assets = append([]ManifestAsset(nil), manifest.Assets...)
	a.updateCache.manifest = manifest
	a.updateCache.manifestKey = key
	a.updateCache.hasManifest = true
	a.updateCache.attemptKey = key
	a.updateCache.attemptedAt = time.Now()
}

func (a *App) maybeAutoCheckUpdates(cfg Config) {
	if !cfg.Update.AutoCheck {
		return
	}
	key := updateManifestCacheKey(cfg)
	now := time.Now()
	a.updateCache.mu.Lock()
	if a.updateCache.checking ||
		(a.updateCache.attemptKey == key && now.Sub(a.updateCache.attemptedAt) < updateAutoCheckInterval) {
		a.updateCache.mu.Unlock()
		return
	}
	a.updateCache.checking = true
	a.updateCache.attemptKey = key
	a.updateCache.attemptedAt = now
	a.updateCache.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), updateManifestTimeout)
		defer cancel()
		manifest, err := fetchManifestContext(ctx, cfg)
		a.updateCache.mu.Lock()
		a.updateCache.checking = false
		if err != nil {
			a.updateCache.mu.Unlock()
			fmt.Fprintf(os.Stderr, "[ao3-hub] automatic update check failed: %v\n", err)
			return
		}
		manifest.Assets = append([]ManifestAsset(nil), manifest.Assets...)
		a.updateCache.manifest = *manifest
		a.updateCache.manifestKey = key
		a.updateCache.hasManifest = true
		a.updateCache.mu.Unlock()
	}()
}

type ApplyResult struct {
	OK       bool   `json:"ok"`
	Version  string `json:"version,omitempty"`
	Message  string `json:"message"`
	Restart  bool   `json:"restart,omitempty"`
	execPath string
}

type ApplyUpdateOptions struct {
	Force        bool
	ForceVersion string
}

func (a *App) ApplyUpdate(opts ApplyUpdateOptions) ApplyResult {
	if !a.updateMu.TryLock() {
		return ApplyResult{OK: false, Message: "已有更新正在进行"}
	}
	defer a.updateMu.Unlock()
	if a.updateRestartPending {
		return ApplyResult{OK: false, Message: "更新已安装，正在等待进程重启"}
	}

	cfg, err := a.store.LoadConfig()
	if err != nil {
		return ApplyResult{OK: false, Message: err.Error()}
	}
	manifest, fetchErr := fetchManifest(cfg)
	if manifest == nil {
		if fetchErr == "" {
			fetchErr = "无法获取 manifest"
		}
		return ApplyResult{OK: false, Message: fetchErr}
	}
	forceVersion := strings.TrimSpace(opts.ForceVersion)
	if forceVersion != "" {
		if !sameVersion(manifest.Version, forceVersion) {
			return ApplyResult{
				OK:      false,
				Message: fmt.Sprintf("manifest 版本 %s 不匹配指定版本 %s", manifest.Version, forceVersion),
			}
		}
		opts.Force = true
	}
	if !opts.Force {
		check := checkForUpdate(cfg.Update.Channel, Version, manifest.Version)
		if !check.HasUpdate {
			return ApplyResult{
				OK:      false,
				Message: fmt.Sprintf("当前 %s 不需要升级到 remote %s：%s", Version, manifest.Version, check.Reason),
			}
		}
	}
	var asset *ManifestAsset
	for i := range manifest.Assets {
		if platformMatch(manifest.Assets[i]) {
			asset = &manifest.Assets[i]
			break
		}
	}
	if asset == nil {
		return ApplyResult{OK: false, Message: fmt.Sprintf("manifest 中无 %s/%s 资源", platformName(), archName())}
	}

	updateDir, err := a.updateDir()
	if err != nil {
		return ApplyResult{OK: false, Message: err.Error()}
	}
	final, err := downloadUpdateAsset(context.Background(), updateDir, manifest.Version, *asset)
	if err != nil {
		return ApplyResult{OK: false, Message: err.Error()}
	}
	defer os.Remove(final)
	execPath, err := installUpdate(final)
	if err != nil {
		return ApplyResult{OK: false, Message: err.Error()}
	}
	a.updateRestartPending = true
	return ApplyResult{
		OK:       true,
		Version:  manifest.Version,
		Message:  fmt.Sprintf("已升级到 %s，进程即将重启", manifest.Version),
		Restart:  true,
		execPath: execPath,
	}
}

func downloadUpdateAsset(parent context.Context, updateDir, version string, asset ManifestAsset) (string, error) {
	if err := validateManifestAsset(asset); err != nil {
		return "", fmt.Errorf("invalid update asset: %w", err)
	}
	if err := os.MkdirAll(updateDir, 0o755); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(parent, updateDownloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, asset.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("accept-encoding", "identity")
	res, err := updateHTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", fmt.Errorf("下载失败: %d", res.StatusCode)
	}
	if res.ContentLength >= 0 && res.ContentLength != asset.Size {
		return "", fmt.Errorf("下载大小不匹配: expected=%d got=%d", asset.Size, res.ContentLength)
	}

	out, err := os.CreateTemp(updateDir, ".ao3-hub-"+safePathSegment(version)+"-*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := out.Name()
	keep := false
	defer func() {
		if out != nil {
			_ = out.Close()
		}
		if !keep {
			_ = os.Remove(tmpPath)
		}
	}()

	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(out, hash), io.LimitReader(res.Body, asset.Size+1))
	if err != nil {
		return "", err
	}
	if written != asset.Size {
		return "", fmt.Errorf("下载大小不匹配: expected=%d got=%d", asset.Size, written)
	}
	gotSHA := hex.EncodeToString(hash.Sum(nil))
	if !strings.EqualFold(gotSHA, asset.SHA256) {
		return "", fmt.Errorf("sha256 校验失败 expected=%s got=%s", asset.SHA256, gotSHA)
	}
	if err := out.Sync(); err != nil {
		return "", fmt.Errorf("sync update download: %w", err)
	}
	if err := out.Chmod(0o755); err != nil {
		return "", fmt.Errorf("chmod update download: %w", err)
	}
	if err := out.Sync(); err != nil {
		return "", fmt.Errorf("sync update metadata: %w", err)
	}
	if err := out.Close(); err != nil {
		out = nil
		return "", err
	}
	out = nil
	finalPath := strings.TrimSuffix(tmpPath, ".tmp")
	if err := os.Rename(tmpPath, finalPath); err != nil {
		return "", fmt.Errorf("finalize update download: %w", err)
	}
	if err := syncDirectory(updateDir); err != nil {
		_ = os.Remove(finalPath)
		return "", fmt.Errorf("sync update directory: %w", err)
	}
	keep = true
	return finalPath, nil
}

func (a *App) updateDir() (string, error) {
	dir, err := dataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "updates"), nil
}

func safePathSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return strconv.FormatInt(time.Now().UnixMilli(), 10)
	}
	var out strings.Builder
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._+-", char) {
			out.WriteRune(char)
		} else {
			out.WriteByte('_')
		}
	}
	return out.String()
}

func installUpdate(newBinaryPath string) (string, error) {
	execPath, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}
	if err := installUpdateAt(newBinaryPath, execPath); err != nil {
		return "", err
	}
	return execPath, nil
}

func installUpdateAt(newBinaryPath, execPath string) error {
	in, err := os.Open(newBinaryPath)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.CreateTemp(filepath.Dir(execPath), "."+filepath.Base(execPath)+"-update-*.tmp")
	if err != nil {
		return err
	}
	preparedPath := out.Name()
	prepared := true
	defer func() {
		if out != nil {
			_ = out.Close()
		}
		if prepared {
			_ = os.Remove(preparedPath)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("stage new binary: %w", err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("sync new binary: %w", err)
	}
	if err := out.Chmod(0o755); err != nil {
		return fmt.Errorf("chmod new binary: %w", err)
	}
	if err := out.Sync(); err != nil {
		return fmt.Errorf("sync new binary metadata: %w", err)
	}
	if err := out.Close(); err != nil {
		out = nil
		return fmt.Errorf("close new binary: %w", err)
	}
	out = nil
	if err := replacePreparedExecutable(preparedPath, execPath); err != nil {
		return err
	}
	prepared = false
	return nil
}

func replacePreparedExecutable(preparedPath, execPath string) error {
	if runtime.GOOS == "windows" {
		return replacePreparedExecutableWithRename(preparedPath, execPath, os.Rename)
	}

	backupPath := execPath + ".bak"
	if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove previous binary backup: %w", err)
	}
	if err := os.Link(execPath, backupPath); err != nil {
		return fmt.Errorf("link current binary backup: %w", err)
	}
	dir := filepath.Dir(execPath)
	if err := syncDirectory(dir); err != nil {
		removeErr := os.Remove(backupPath)
		return errors.Join(fmt.Errorf("sync binary backup: %w", err), removeErr)
	}
	if err := os.Rename(preparedPath, execPath); err != nil {
		removeErr := os.Remove(backupPath)
		return errors.Join(fmt.Errorf("install prepared binary: %w", err), removeErr)
	}
	if err := syncDirectory(dir); err != nil {
		installErr := fmt.Errorf("sync installed binary: %w", err)
		if rollbackErr := os.Rename(backupPath, execPath); rollbackErr != nil {
			return errors.Join(installErr, fmt.Errorf("rollback current binary: %w", rollbackErr))
		}
		if rollbackSyncErr := syncDirectory(dir); rollbackSyncErr != nil {
			return errors.Join(installErr, fmt.Errorf("sync binary rollback: %w", rollbackSyncErr))
		}
		return installErr
	}
	return nil
}

// Windows cannot atomically replace a running executable with os.Rename. Keep
// the prepared-file and explicit rollback path for future Windows packages.
func replacePreparedExecutableWithRename(preparedPath, execPath string, rename func(string, string) error) error {
	backupPath := execPath + ".bak"
	if err := os.Remove(backupPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove previous binary backup: %w", err)
	}
	if err := rename(execPath, backupPath); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}
	if err := rename(preparedPath, execPath); err != nil {
		installErr := fmt.Errorf("install prepared binary: %w", err)
		if rollbackErr := rename(backupPath, execPath); rollbackErr != nil {
			return errors.Join(installErr, fmt.Errorf("rollback current binary: %w", rollbackErr))
		}
		return installErr
	}
	return nil
}

func syncDirectory(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}

func scheduleExec(delayMS int, execPath string) {
	if execPath == "" {
		fmt.Fprintf(os.Stderr, "[ao3-hub] restart failed: missing executable path\n")
		return
	}
	if delayMS < 0 {
		delayMS = 600
	}
	go func() {
		time.Sleep(time.Duration(delayMS) * time.Millisecond)
		fmt.Fprintf(os.Stderr, "[ao3-hub] restarting with updated binary\n")
		if err := execCurrentProcess(execPath); err != nil {
			fmt.Fprintf(os.Stderr, "[ao3-hub] restart failed: %v\n", err)
		}
	}()
}
