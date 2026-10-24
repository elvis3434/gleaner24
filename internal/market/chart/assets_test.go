package chart

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The vendored lightweight-charts.js must be the version declared in
// assets/package.json, which Dependabot bumps. A failure here after a
// Dependabot update means the file has not been re-vendored yet: run
// `make update-lwc` (see docs/DEPENDENCIES.md).
func TestVendoredChartLibraryMatchesManifest(t *testing.T) {
	var pkg struct {
		Dependencies map[string]string `json:"dependencies"`
	}
	b, err := os.ReadFile(filepath.Join("assets", "package.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		t.Fatal(err)
	}
	want := pkg.Dependencies["lightweight-charts"]
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(want) {
		t.Fatalf("package.json must pin an exact lightweight-charts version, got %q", want)
	}

	js, err := assets.ReadFile("assets/lightweight-charts.js")
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`TradingView Lightweight Charts™ v(\d+\.\d+\.\d+)`).FindSubmatch(js[:min(len(js), 512)])
	if m == nil {
		t.Fatal("lightweight-charts.js has no version header")
	}
	if got := string(m[1]); got != want {
		t.Errorf("vendored lightweight-charts.js is %s but package.json declares %s: run make update-lwc", got, want)
	}

	lic, err := assets.ReadFile("assets/LICENSE-lightweight-charts.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lic), "lightweight-charts "+want+" from the npm registry") {
		t.Errorf("LICENSE-lightweight-charts.txt does not describe version %s", want)
	}
	for _, need := range []string{"Apache License", "https://www.tradingview.com/", "fancy-canvas"} {
		if !strings.Contains(string(lic), need) {
			t.Errorf("license notice lacks %q", need)
		}
	}

	var lock struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
	}
	b, err = os.ReadFile(filepath.Join("assets", "package-lock.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(b, &lock); err != nil {
		t.Fatal(err)
	}
	if got := lock.Packages["node_modules/lightweight-charts"].Version; got != want {
		t.Errorf("package-lock.json has %s, package.json %s", got, want)
	}
}
