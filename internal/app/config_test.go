package app

import (
	"encoding/json"
	"os"
	"testing"
)

func TestLoadConfigMaterializesTemplate(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg != defaultConfig() {
		t.Fatalf("first load = %+v, want the hardcoded template", cfg)
	}
	raw, err := os.ReadFile(store.path("config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var written Config
	if err := json.Unmarshal(raw, &written); err != nil {
		t.Fatal(err)
	}
	if written != defaultConfig() {
		t.Fatalf("written config = %+v, want the hardcoded template", written)
	}
}

func TestLoadConfigCompletesMissingKeys(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	partial := `{"server":{"port":4100},"llm":{"apiKey":"secret","temperature":0,"stream":false},"update":{"autoCheck":true}}`
	if err := store.writeFile(store.path("config.json"), []byte(partial)); err != nil {
		t.Fatal(err)
	}
	cfg, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}

	want := defaultConfig()
	want.Server.Port = 4100
	want.LLM.APIKey = "secret"
	want.LLM.Temperature = 0
	want.Update.AutoCheck = true
	if cfg != want {
		t.Fatalf("completed config = %+v, want %+v", cfg, want)
	}

	raw, err := os.ReadFile(store.path("config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"server", "auth", "stream", "import", "ui", "llm", "ao3", "reader", "update"} {
		if _, ok := onDisk[section]; !ok {
			t.Fatalf("section %q missing after completion", section)
		}
	}

	stable, err := store.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if stable != cfg {
		t.Fatalf("second load = %+v, want %+v", stable, cfg)
	}
}

func TestLoadConfigRejectsInvalidPublicOrigin(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.Server.PublicOrigin = "https://ao3hub.example/path"
	if err := store.writeJSON(store.path("config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := store.LoadConfig(); err == nil {
		t.Fatal("accepted invalid server.publicOrigin")
	}
}
