package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	stableVersionRE = regexp.MustCompile(`^v?(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$`)
	devVersionRE    = regexp.MustCompile(`^dev-(\d+)-\d{8}-([A-Za-z0-9]+)(?:[-+].*)?$`)
)

type updateCheck struct {
	HasUpdate bool
	Strategy  string
	Reason    string
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
	url := resolveManifestURL(cfg)
	if url == "" {
		return nil, "未配置 manifest URL"
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err.Error()
	}
	req.Header.Set("cache-control", "no-cache")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err.Error()
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return nil, fmt.Sprintf("manifest fetch failed: %d", res.StatusCode)
	}
	var manifest Manifest
	if err := json.NewDecoder(res.Body).Decode(&manifest); err != nil {
		return nil, "manifest schema 校验失败"
	}
	if manifest.Version == "" || manifest.Assets == nil {
		return nil, "manifest schema 校验失败"
	}
	return &manifest, ""
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
	manifest, _ := fetchManifest(cfg)
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

func sha256HexFile(file string) (string, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

type ApplyResult struct {
	OK      bool   `json:"ok"`
	Version string `json:"version,omitempty"`
	Message string `json:"message"`
	Restart bool   `json:"restart,omitempty"`
}

type ApplyUpdateOptions struct {
	Force        bool
	ForceVersion string
}

func (a *App) ApplyUpdate(opts ApplyUpdateOptions) ApplyResult {
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
	if err := os.MkdirAll(updateDir, 0o755); err != nil {
		return ApplyResult{OK: false, Message: err.Error()}
	}
	tmp := filepath.Join(updateDir, fmt.Sprintf("ao3-hub-%s.tmp", safePathSegment(manifest.Version)))
	final := filepath.Join(updateDir, fmt.Sprintf("ao3-hub-%s", safePathSegment(manifest.Version)))

	req, err := http.NewRequest(http.MethodGet, asset.URL, nil)
	if err != nil {
		return ApplyResult{OK: false, Message: err.Error()}
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return ApplyResult{OK: false, Message: err.Error()}
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return ApplyResult{OK: false, Message: fmt.Sprintf("下载失败: %d", res.StatusCode)}
	}
	out, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o755)
	if err != nil {
		return ApplyResult{OK: false, Message: err.Error()}
	}
	if _, err := io.Copy(out, res.Body); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return ApplyResult{OK: false, Message: err.Error()}
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return ApplyResult{OK: false, Message: err.Error()}
	}

	if asset.SHA256 != "" {
		sum, err := sha256HexFile(tmp)
		if err != nil {
			_ = os.Remove(tmp)
			return ApplyResult{OK: false, Message: err.Error()}
		}
		if !strings.EqualFold(sum, asset.SHA256) {
			_ = os.Remove(tmp)
			return ApplyResult{OK: false, Message: fmt.Sprintf("sha256 校验失败 expected=%s got=%s", asset.SHA256, sum)}
		}
	}
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return ApplyResult{OK: false, Message: err.Error()}
	}
	_ = os.Remove(final)
	if err := os.Rename(tmp, final); err != nil {
		_ = os.Remove(tmp)
		return ApplyResult{OK: false, Message: err.Error()}
	}
	if err := installUpdate(final); err != nil {
		return ApplyResult{OK: false, Message: err.Error()}
	}
	return ApplyResult{
		OK:      true,
		Version: manifest.Version,
		Message: fmt.Sprintf("已升级到 %s，进程即将重启", manifest.Version),
		Restart: true,
	}
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
	replacer := strings.NewReplacer("/", "_", "\\", "_", ":", "_")
	return replacer.Replace(value)
}

func installUpdate(newBinaryPath string) error {
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
		execPath = resolved
	}
	backupPath := execPath + ".bak"

	_ = os.Remove(backupPath)
	if err := os.Rename(execPath, backupPath); err != nil {
		return fmt.Errorf("backup current binary: %w", err)
	}
	if err := copyFile(newBinaryPath, execPath); err != nil {
		_ = os.Rename(backupPath, execPath)
		return fmt.Errorf("install new binary: %w", err)
	}
	if err := os.Chmod(execPath, 0o755); err != nil {
		_ = os.Rename(backupPath, execPath)
		_ = os.Remove(newBinaryPath)
		return fmt.Errorf("chmod new binary: %w", err)
	}
	_ = os.Remove(backupPath)
	_ = os.Remove(newBinaryPath)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func scheduleExec(delayMS int) {
	if delayMS < 0 {
		delayMS = 600
	}
	go func() {
		time.Sleep(time.Duration(delayMS) * time.Millisecond)
		execPath, err := os.Executable()
		if err != nil {
			fmt.Fprintf(os.Stderr, "[ao3-hub] restart failed: resolve executable path: %v\n", err)
			return
		}
		if resolved, err := filepath.EvalSymlinks(execPath); err == nil {
			execPath = resolved
		}
		fmt.Fprintf(os.Stderr, "[ao3-hub] restarting with updated binary\n")
		if err := execCurrentProcess(execPath); err != nil {
			fmt.Fprintf(os.Stderr, "[ao3-hub] restart failed: %v\n", err)
		}
	}()
}
