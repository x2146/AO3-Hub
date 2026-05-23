package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

type asset struct {
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
}

type manifest struct {
	Version     string  `json:"version"`
	Channel     string  `json:"channel"`
	Notes       string  `json:"notes"`
	PublishedAt string  `json:"publishedAt"`
	Assets      []asset `json:"assets"`
}

type options struct {
	repo        string
	tag         string
	out         string
	baseURL     string
	channel     string
	version     string
	notes       string
	publishedAt string
	files       []string
}

var assetNameRE = regexp.MustCompile(`^ao3-hub-(darwin|linux|windows|win32)-(x64|arm64)(?:\.exe)?$`)

func main() {
	opts, err := parseArgs(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		usage()
		os.Exit(1)
	}
	if err := run(opts); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseArgs(args []string) (options, error) {
	fs := flag.NewFlagSet("ao3hub-manifest", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	opts := options{}
	fs.StringVar(&opts.repo, "repo", "", "GitHub repository used for release asset URLs")
	fs.StringVar(&opts.tag, "tag", "", "GitHub release tag")
	fs.StringVar(&opts.out, "out", "", "manifest output path")
	fs.StringVar(&opts.baseURL, "base-url", "https://github.com", "GitHub base URL")
	fs.StringVar(&opts.channel, "channel", "stable", "manifest channel")
	fs.StringVar(&opts.version, "version", "", "manifest version")
	fs.StringVar(&opts.notes, "notes", "", "manifest notes")
	fs.StringVar(&opts.publishedAt, "published-at", "", "manifest publishedAt")

	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	opts.files = fs.Args()
	if opts.repo == "" || opts.tag == "" || opts.out == "" || len(opts.files) == 0 {
		return opts, errors.New("missing required arguments")
	}
	if !regexp.MustCompile(`^[^/]+/[^/]+$`).MatchString(opts.repo) {
		return opts, fmt.Errorf("invalid --repo value: %s", opts.repo)
	}
	if opts.version == "" {
		version, err := packageVersion()
		if err != nil {
			return opts, err
		}
		opts.version = version
	}
	if opts.notes == "" {
		opts.notes = "GitHub release " + opts.tag
	}
	if opts.publishedAt == "" {
		opts.publishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return opts, nil
}

func usage() {
	fmt.Fprintln(os.Stderr, strings.Join([]string{
		"Usage:",
		"  go run ./cmd/ao3hub-manifest --repo owner/repo --tag v0.1.1 --out manifest.json <files...>",
		"",
		"Options:",
		"  --repo owner/repo        GitHub repository used for release asset URLs",
		"  --tag vX.Y.Z             GitHub release tag",
		"  --out file               Manifest output path",
		"  --base-url url           GitHub base URL, defaults to https://github.com",
		"  --channel name           Manifest channel, defaults to stable",
		"  --version X.Y.Z          Manifest version",
		"  --notes text             Manifest notes",
		"  --published-at date      Manifest publishedAt, defaults to current time",
	}, "\n"))
}

func run(opts options) error {
	baseURL := strings.TrimRight(opts.baseURL, "/")
	releaseURL := fmt.Sprintf("%s/%s/releases/download/%s", baseURL, opts.repo, url.PathEscape(opts.tag))

	assets := make([]asset, 0, len(opts.files))
	for _, file := range opts.files {
		platform, arch, err := assetTarget(file)
		if err != nil {
			return err
		}
		info, err := os.Stat(file)
		if err != nil {
			return err
		}
		sum, err := sha256HexFile(file)
		if err != nil {
			return err
		}
		name := filepath.Base(file)
		assets = append(assets, asset{
			Platform: platform,
			Arch:     arch,
			URL:      releaseURL + "/" + url.PathEscape(name),
			SHA256:   sum,
			Size:     info.Size(),
		})
	}

	sort.Slice(assets, func(i, j int) bool {
		return assets[i].Platform+"/"+assets[i].Arch < assets[j].Platform+"/"+assets[j].Arch
	})

	body := manifest{
		Version:     opts.version,
		Channel:     opts.channel,
		Notes:       opts.notes,
		PublishedAt: opts.publishedAt,
		Assets:      assets,
	}
	if err := os.MkdirAll(filepath.Dir(opts.out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(opts.out)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(body); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Printf("[ota] wrote %s with %d assets\n", opts.out, len(assets))
	return nil
}

func packageVersion() (string, error) {
	file, err := os.Open("package.json")
	if err != nil {
		return "", err
	}
	defer file.Close()

	var pkg struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(file).Decode(&pkg); err != nil {
		return "", err
	}
	if pkg.Version == "" {
		return "0.0.0", nil
	}
	return pkg.Version, nil
}

func assetTarget(file string) (string, string, error) {
	name := filepath.Base(file)
	match := assetNameRE.FindStringSubmatch(name)
	if match == nil {
		return "", "", fmt.Errorf("cannot infer platform/arch from %s; expected ao3-hub-<platform>-<arch>", name)
	}
	platform := match[1]
	if platform == "win32" {
		platform = "windows"
	}
	return platform, match[2], nil
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
