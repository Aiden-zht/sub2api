package handler

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestThemeInstallZipAcceptsSafePackage(t *testing.T) {
	h := NewThemeHandler(t.TempDir())
	zipPath := writeThemeZip(t, map[string]string{
		"manifest.json": `{
			"id": "clean_theme",
			"name": "Clean Theme",
			"version": "1.0.0",
			"sub2apiThemeApi": "1",
			"entry": "theme.css",
			"tokens": { "colorPrimary500": "#2563eb" }
		}`,
		"theme.css":                  `:root { --s2a-color-primary-500: #2563eb; }`,
		"assets/logo.webp":           "webp",
		"fonts/inter-variable.woff2": "font",
	})

	record, err := h.installZip(zipPath)
	if err != nil {
		t.Fatalf("installZip returned error: %v", err)
	}
	if record.ID != "clean_theme" || record.Name != "Clean Theme" {
		t.Fatalf("unexpected installed theme record: %#v", record)
	}
	if _, err := os.Stat(filepath.Join(h.themeDir("clean_theme"), "theme.css")); err != nil {
		t.Fatalf("theme css was not extracted: %v", err)
	}

	records, err := h.loadIndex()
	if err != nil {
		t.Fatalf("loadIndex returned error: %v", err)
	}
	if len(records) != 1 || records[0].ID != "clean_theme" {
		t.Fatalf("unexpected index records: %#v", records)
	}
}

func TestThemeInstallZipRejectsExecutableFrontendCode(t *testing.T) {
	h := NewThemeHandler(t.TempDir())
	zipPath := writeThemeZip(t, map[string]string{
		"manifest.json": `{
			"id": "script_theme",
			"name": "Script Theme",
			"version": "1.0.0",
			"entry": "theme.css"
		}`,
		"theme.css": "body {}",
		"theme.js":  "alert('nope')",
	})

	_, err := h.installZip(zipPath)
	if err == nil || !strings.Contains(err.Error(), "executable frontend code") {
		t.Fatalf("expected executable code rejection, got %v", err)
	}
}

func TestThemeInstallZipRejectsPathTraversal(t *testing.T) {
	h := NewThemeHandler(t.TempDir())
	zipPath := writeThemeZip(t, map[string]string{
		"manifest.json": `{
			"id": "bad_path",
			"name": "Bad Path",
			"version": "1.0.0",
			"entry": "theme.css"
		}`,
		"theme.css":       "body {}",
		"assets/../x.css": "body {}",
	})

	_, err := h.installZip(zipPath)
	if err == nil || !strings.Contains(err.Error(), "invalid theme path") {
		t.Fatalf("expected path traversal rejection, got %v", err)
	}
}

func TestThemeInstallZipRejectsOversizedExpandedPayload(t *testing.T) {
	h := NewThemeHandler(t.TempDir())
	zipPath := writeThemeZip(t, map[string]string{
		"manifest.json": `{
			"id": "big_theme",
			"name": "Big Theme",
			"version": "1.0.0",
			"entry": "theme.css"
		}`,
		"theme.css": strings.Repeat("a", themeMaxUploadSize+1),
	})

	_, err := h.installZip(zipPath)
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("expected oversized theme rejection, got %v", err)
	}
}

func TestThemeInstallZipRejectsSVGAssets(t *testing.T) {
	h := NewThemeHandler(t.TempDir())
	zipPath := writeThemeZip(t, map[string]string{
		"manifest.json": `{
			"id": "svg_theme",
			"name": "SVG Theme",
			"version": "1.0.0",
			"entry": "theme.css"
		}`,
		"theme.css":       "body {}",
		"assets/logo.svg": "<svg></svg>",
	})

	_, err := h.installZip(zipPath)
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("expected svg rejection, got %v", err)
	}
}

func TestThemeInstallZipRejectsCSSImport(t *testing.T) {
	h := NewThemeHandler(t.TempDir())
	zipPath := writeThemeZip(t, map[string]string{
		"manifest.json": `{
			"id": "import_theme",
			"name": "Import Theme",
			"version": "1.0.0",
			"entry": "theme.css"
		}`,
		"theme.css": `@import url("https://cdn.example.com/theme.css");`,
	})

	_, err := h.installZip(zipPath)
	if err == nil || !strings.Contains(err.Error(), "cannot use @import") {
		t.Fatalf("expected css import rejection, got %v", err)
	}
}

func TestThemeInstallZipRejectsExternalCSSAssetURL(t *testing.T) {
	h := NewThemeHandler(t.TempDir())
	zipPath := writeThemeZip(t, map[string]string{
		"manifest.json": `{
			"id": "remote_asset_theme",
			"name": "Remote Asset Theme",
			"version": "1.0.0",
			"entry": "theme.css"
		}`,
		"theme.css": `.hero { background-image: url("https://cdn.example.com/hero.webp"); }`,
	})

	_, err := h.installZip(zipPath)
	if err == nil || !strings.Contains(err.Error(), "cannot load external assets") {
		t.Fatalf("expected external asset rejection, got %v", err)
	}
}

func TestThemeInstallZipRejectsMissingLocalCSSAsset(t *testing.T) {
	h := NewThemeHandler(t.TempDir())
	zipPath := writeThemeZip(t, map[string]string{
		"manifest.json": `{
			"id": "missing_asset_theme",
			"name": "Missing Asset Theme",
			"version": "1.0.0",
			"entry": "theme.css"
		}`,
		"theme.css": `.hero { background-image: url("./assets/hero.webp"); }`,
	})

	_, err := h.installZip(zipPath)
	if err == nil || !strings.Contains(err.Error(), "invalid asset path") {
		t.Fatalf("expected missing asset rejection, got %v", err)
	}
}

func writeThemeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "theme.zip")
	out, err := os.Create(path)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(out)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("write zip entry %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := out.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
	return path
}
