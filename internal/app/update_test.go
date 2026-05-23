package app

import "testing"

func TestCheckForUpdateStableStrategy(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		remote     string
		hasUpdate  bool
		wantReason string
	}{
		{name: "newer stable", current: "v0.1.0", remote: "v0.1.1", hasUpdate: true, wantReason: "远端 stable 版本更新"},
		{name: "same stable ignores v prefix", current: "0.1.1", remote: "v0.1.1", wantReason: "版本相同"},
		{name: "older stable", current: "v0.2.0", remote: "v0.1.1", wantReason: "当前 stable 版本已是最新"},
		{name: "stable can replace dev build", current: "dev-0012-20260523-abcdef0", remote: "v0.1.1", hasUpdate: true, wantReason: "当前为 dev 构建，允许切换到 stable"},
		{name: "stable rejects dev remote", current: "v0.1.0", remote: "dev-0012-20260523-abcdef0", wantReason: "stable channel 需要远端版本是 semver"},
		{name: "unknown current is not comparable", current: "nightly", remote: "v0.1.1", wantReason: "当前版本不是可比较的 semver"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkForUpdate("stable", tt.current, tt.remote)
			if got.Strategy != "stable-semver" {
				t.Fatalf("strategy = %q, want stable-semver", got.Strategy)
			}
			if got.HasUpdate != tt.hasUpdate {
				t.Fatalf("hasUpdate = %v, want %v", got.HasUpdate, tt.hasUpdate)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestCheckForUpdateDevStrategy(t *testing.T) {
	tests := []struct {
		name       string
		current    string
		remote     string
		hasUpdate  bool
		wantReason string
	}{
		{name: "newer dev run", current: "dev-0007-20260401-aaaaaaa", remote: "dev-0042-20260425-bbbbbbb", hasUpdate: true, wantReason: "远端 dev run number 更新"},
		{name: "same commit sha", current: "dev-0007-20260401-aaaaaaa", remote: "dev-0042-20260425-aaaaaaa", wantReason: "commit SHA 相同"},
		{name: "older dev run", current: "dev-0042-20260425-bbbbbbb", remote: "dev-0007-20260401-aaaaaaa", wantReason: "当前 dev run number 已是最新"},
		{name: "local dev accepts ci dev", current: "dev-local", remote: "dev-0042-20260425-bbbbbbb", hasUpdate: true, wantReason: "本地 dev 版本未注入 CI run，允许升级"},
		{name: "stable does not auto switch to dev", current: "v0.1.1", remote: "dev-0042-20260425-bbbbbbb", wantReason: "当前版本不是 dev CI tag，避免自动切换或回退"},
		{name: "invalid remote dev", current: "dev-0007-20260401-aaaaaaa", remote: "v0.1.1", wantReason: "dev channel 需要远端版本是 dev CI tag"},
		{name: "unknown dev current", current: "dev-preview", remote: "dev-0042-20260425-bbbbbbb", wantReason: "当前版本不是 dev CI tag，避免自动切换或回退"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := checkForUpdate("dev", tt.current, tt.remote)
			if got.Strategy != "dev-run" {
				t.Fatalf("strategy = %q, want dev-run", got.Strategy)
			}
			if got.HasUpdate != tt.hasUpdate {
				t.Fatalf("hasUpdate = %v, want %v", got.HasUpdate, tt.hasUpdate)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestSameVersionIgnoresStableVPrefix(t *testing.T) {
	if !sameVersion("v0.1.1", "0.1.1") {
		t.Fatal("expected v-prefixed stable versions to match")
	}
	if sameVersion("dev-0012-20260523-abcdef0", "dev-0013-20260523-abcdef0") {
		t.Fatal("expected different dev action versions not to match")
	}
}
