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
	"strconv"
	"strings"
	"time"
)

func compareSemver(a, b string) int {
	pa := strings.Split(strings.TrimPrefix(a, "v"), ".")
	pb := strings.Split(strings.TrimPrefix(b, "v"), ".")
	n := len(pa)
	if len(pb) > n {
		n = len(pb)
	}
	for i := 0; i < n; i++ {
		x, y := 0, 0
		if i < len(pa) {
			x, _ = strconv.Atoi(pa[i])
		}
		if i < len(pb) {
			y, _ = strconv.Atoi(pb[i])
		}
		if x != y {
			return x - y
		}
	}
	return 0
}

func isDevVersion(version string) bool {
	return strings.HasPrefix(version, "dev-")
}

func compareVersions(a, b string) int {
	if a == b {
		return 0
	}
	if isDevVersion(a) || isDevVersion(b) {
		return 1
	}
	return compareSemver(a, b)
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

func (a *App) FetchManifest() (*Manifest, string) {
	cfg, err := a.store.LoadConfig()
	if err != nil {
		return nil, err.Error()
	}
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

func (a *App) VersionInfo() VersionInfo {
	base := VersionInfo{
		Current:  Version,
		Platform: platformName(),
		Arch:     archName(),
		BuiltAt:  BuiltAt,
	}
	manifest, _ := a.FetchManifest()
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
	latest := &LatestVersion{
		Version:     manifest.Version,
		Channel:     manifest.Channel,
		Notes:       manifest.Notes,
		PublishedAt: manifest.PublishedAt,
		HasUpdate:   compareVersions(manifest.Version, Version) > 0,
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

func (a *App) ApplyUpdate(force bool) ApplyResult {
	manifest, fetchErr := a.FetchManifest()
	if manifest == nil {
		if fetchErr == "" {
			fetchErr = "无法获取 manifest"
		}
		return ApplyResult{OK: false, Message: fetchErr}
	}
	if !force && compareVersions(manifest.Version, Version) <= 0 {
		return ApplyResult{OK: false, Message: fmt.Sprintf("当前 %s 已是最新（remote %s）", Version, manifest.Version)}
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
	exec, err := os.Executable()
	if err != nil {
		return ApplyResult{OK: false, Message: err.Error()}
	}
	dir := filepath.Dir(exec)
	tmp := filepath.Join(dir, fmt.Sprintf(".ao3-hub.new-%d", time.Now().UnixMilli()))

	res, err := http.Get(asset.URL)
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
	_ = out.Close()

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
	_ = os.Chmod(tmp, 0o755)
	backup := filepath.Join(dir, ".ao3-hub.bak-"+Version)
	_ = os.Rename(exec, backup)
	if err := os.Rename(tmp, exec); err != nil {
		return ApplyResult{OK: false, Message: err.Error()}
	}
	return ApplyResult{
		OK:      true,
		Version: manifest.Version,
		Message: fmt.Sprintf("已升级到 %s，进程即将退出，等待 launcher 重启", manifest.Version),
		Restart: true,
	}
}

func scheduleExit(delayMS int) {
	if delayMS < 0 {
		delayMS = 600
	}
	go func() {
		time.Sleep(time.Duration(delayMS) * time.Millisecond)
		os.Exit(0)
	}()
}
