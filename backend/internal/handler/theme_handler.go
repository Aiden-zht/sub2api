package handler

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

const (
	themeMaxUploadSize = 20 << 20
	themeMaxFiles      = 200
	themeIndexFile     = "index.json"
)

var (
	themeSlugPattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,62}$`)
	allowedThemeExtension  = map[string]struct{}{".json": {}, ".css": {}, ".png": {}, ".jpg": {}, ".jpeg": {}, ".webp": {}, ".ico": {}, ".woff2": {}}
	disallowedThemeEntries = map[string]struct{}{".js": {}, ".mjs": {}, ".cjs": {}, ".ts": {}, ".tsx": {}, ".jsx": {}, ".vue": {}, ".html": {}, ".htm": {}, ".wasm": {}, ".sh": {}, ".exe": {}, ".dll": {}, ".dylib": {}, ".so": {}}
	cssImportPattern       = regexp.MustCompile(`(?i)@import`)
	cssURLPattern          = regexp.MustCompile(`(?i)url\(([^)]+)\)`)
)

type ThemeHandler struct {
	baseDir string
}

type ThemeManifest struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Version         string            `json:"version"`
	Sub2APIThemeAPI string            `json:"sub2apiThemeApi"`
	Author          string            `json:"author,omitempty"`
	Description     string            `json:"description,omitempty"`
	Entry           string            `json:"entry"`
	Assets          map[string]string `json:"assets,omitempty"`
	Tokens          map[string]string `json:"tokens,omitempty"`
	Capabilities    []string          `json:"capabilities,omitempty"`
}

type ThemePackageRecord struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Version     string        `json:"version"`
	Manifest    ThemeManifest `json:"manifest"`
	StoragePath string        `json:"-"`
	Enabled     bool          `json:"enabled"`
	InstalledAt time.Time     `json:"installed_at"`
	UpdatedAt   time.Time     `json:"updated_at"`
}

type ActiveThemeResponse struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	EntryCSSURL string            `json:"entry_css_url"`
	Tokens      map[string]string `json:"tokens,omitempty"`
}

func NewThemeHandler(dataDir string) *ThemeHandler {
	return &ThemeHandler{baseDir: filepath.Join(dataDir, "themes")}
}

func RegisterThemeRoutes(v1 *gin.RouterGroup, dataDir string, adminAuth gin.HandlerFunc, settingService *service.SettingService) {
	h := NewThemeHandler(dataDir)

	public := v1.Group("/themes")
	{
		public.GET("/active", h.GetActiveTheme)
		public.GET("/assets/:slug/*filename", h.ServeThemeAsset)
	}

	adminThemes := v1.Group("/admin/themes")
	adminThemes.Use(adminAuth)
	adminThemes.Use(middleware2.AdminComplianceGuard(settingService))
	{
		adminThemes.GET("", h.ListThemes)
		adminThemes.POST("/upload", h.UploadTheme)
		adminThemes.POST("/:id/enable", h.EnableTheme)
		adminThemes.POST("/:id/disable", h.DisableTheme)
		adminThemes.DELETE("/:id", h.DeleteTheme)
	}
}

func (h *ThemeHandler) ListThemes(c *gin.Context) {
	records, err := h.loadIndex()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, fmt.Sprintf("load themes: %v", err))
		return
	}
	response.Success(c, gin.H{"themes": records})
}

func (h *ThemeHandler) GetActiveTheme(c *gin.Context) {
	records, err := h.loadIndex()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, fmt.Sprintf("load themes: %v", err))
		return
	}
	for _, record := range records {
		if !record.Enabled {
			continue
		}
		response.Success(c, ActiveThemeResponse{
			ID:          record.ID,
			Name:        record.Name,
			Version:     record.Version,
			EntryCSSURL: themeAssetURL(record.ID, record.Manifest.Entry),
			Tokens:      record.Manifest.Tokens,
		})
		return
	}
	response.Success(c, nil)
}

func (h *ThemeHandler) UploadTheme(c *gin.Context) {
	if err := c.Request.ParseMultipartForm(themeMaxUploadSize); err != nil {
		response.Error(c, http.StatusBadRequest, "invalid theme upload")
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, "theme file is required")
		return
	}
	if file.Size <= 0 || file.Size > themeMaxUploadSize {
		response.Error(c, http.StatusBadRequest, "theme file is too large")
		return
	}
	src, err := file.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, "failed to open theme file")
		return
	}
	defer func() { _ = src.Close() }()

	tmp, err := os.CreateTemp("", "sub2api-theme-*.zip")
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "failed to create temp file")
		return
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if _, err := io.Copy(tmp, io.LimitReader(src, themeMaxUploadSize+1)); err != nil {
		_ = tmp.Close()
		response.Error(c, http.StatusBadRequest, "failed to read theme file")
		return
	}
	_ = tmp.Close()

	record, err := h.installZip(tmpPath)
	if err != nil {
		response.Error(c, http.StatusBadRequest, err.Error())
		return
	}
	response.Created(c, record)
}

func (h *ThemeHandler) EnableTheme(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	records, err := h.loadIndex()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, fmt.Sprintf("load themes: %v", err))
		return
	}
	found := false
	for i := range records {
		records[i].Enabled = records[i].ID == id
		if records[i].Enabled {
			records[i].UpdatedAt = time.Now().UTC()
			found = true
		}
	}
	if !found {
		response.Error(c, http.StatusNotFound, "theme not found")
		return
	}
	if err := h.saveIndex(records); err != nil {
		response.Error(c, http.StatusInternalServerError, fmt.Sprintf("save themes: %v", err))
		return
	}
	response.Success(c, gin.H{"enabled": id})
}

func (h *ThemeHandler) DisableTheme(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	records, err := h.loadIndex()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, fmt.Sprintf("load themes: %v", err))
		return
	}
	found := false
	for i := range records {
		if records[i].ID == id {
			records[i].Enabled = false
			records[i].UpdatedAt = time.Now().UTC()
			found = true
		}
	}
	if !found {
		response.Error(c, http.StatusNotFound, "theme not found")
		return
	}
	if err := h.saveIndex(records); err != nil {
		response.Error(c, http.StatusInternalServerError, fmt.Sprintf("save themes: %v", err))
		return
	}
	response.Success(c, gin.H{"disabled": id})
}

func (h *ThemeHandler) DeleteTheme(c *gin.Context) {
	id := strings.TrimSpace(c.Param("id"))
	records, err := h.loadIndex()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, fmt.Sprintf("load themes: %v", err))
		return
	}
	next := records[:0]
	found := false
	for _, record := range records {
		if record.ID == id {
			found = true
			_ = os.RemoveAll(h.themeDir(record.ID))
			continue
		}
		next = append(next, record)
	}
	if !found {
		response.Error(c, http.StatusNotFound, "theme not found")
		return
	}
	if err := h.saveIndex(next); err != nil {
		response.Error(c, http.StatusInternalServerError, fmt.Sprintf("save themes: %v", err))
		return
	}
	response.Success(c, gin.H{"deleted": id})
}

func (h *ThemeHandler) ServeThemeAsset(c *gin.Context) {
	slug := strings.TrimSpace(c.Param("slug"))
	filename := strings.TrimPrefix(c.Param("filename"), "/")
	path, ok := h.resolveThemeAssetPath(slug, filename)
	if !ok {
		c.String(http.StatusNotFound, "not found")
		return
	}
	c.File(path)
}

func (h *ThemeHandler) installZip(zipPath string) (ThemePackageRecord, error) {
	reader, err := zip.OpenReader(zipPath)
	if err != nil {
		return ThemePackageRecord{}, fmt.Errorf("invalid theme zip")
	}
	defer func() { _ = reader.Close() }()
	if len(reader.File) == 0 || len(reader.File) > themeMaxFiles {
		return ThemePackageRecord{}, fmt.Errorf("invalid theme file count")
	}
	manifestFile := findThemeManifest(reader.File)
	if manifestFile == nil {
		return ThemePackageRecord{}, fmt.Errorf("manifest.json is required")
	}
	manifest, err := readThemeManifest(manifestFile)
	if err != nil {
		return ThemePackageRecord{}, err
	}
	if err := validateThemeManifest(manifest); err != nil {
		return ThemePackageRecord{}, err
	}

	targetDir := h.themeDir(manifest.ID)
	stagingDir := targetDir + ".staging"
	_ = os.RemoveAll(stagingDir)
	if err := os.MkdirAll(stagingDir, 0o755); err != nil {
		return ThemePackageRecord{}, fmt.Errorf("create theme directory: %w", err)
	}
	if err := extractThemeZip(reader.File, stagingDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return ThemePackageRecord{}, err
	}
	entryPath, ok := resolvePathWithin(stagingDir, manifest.Entry)
	if !ok {
		_ = os.RemoveAll(stagingDir)
		return ThemePackageRecord{}, fmt.Errorf("theme entry is invalid")
	}
	if err := validateExtractedThemeCSS(stagingDir, entryPath); err != nil {
		_ = os.RemoveAll(stagingDir)
		return ThemePackageRecord{}, err
	}
	_ = os.RemoveAll(targetDir)
	if err := os.Rename(stagingDir, targetDir); err != nil {
		_ = os.RemoveAll(stagingDir)
		return ThemePackageRecord{}, fmt.Errorf("install theme: %w", err)
	}

	records, err := h.loadIndex()
	if err != nil {
		return ThemePackageRecord{}, err
	}
	now := time.Now().UTC()
	record := ThemePackageRecord{
		ID:          manifest.ID,
		Name:        manifest.Name,
		Version:     manifest.Version,
		Manifest:    manifest,
		StoragePath: targetDir,
		InstalledAt: now,
		UpdatedAt:   now,
	}
	updated := false
	for i := range records {
		if records[i].ID == record.ID {
			record.Enabled = records[i].Enabled
			record.InstalledAt = records[i].InstalledAt
			records[i] = record
			updated = true
			break
		}
	}
	if !updated {
		records = append(records, record)
	}
	if err := h.saveIndex(records); err != nil {
		return ThemePackageRecord{}, err
	}
	return record, nil
}

func (h *ThemeHandler) loadIndex() ([]ThemePackageRecord, error) {
	path := h.indexPath()
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []ThemePackageRecord{}, nil
		}
		return nil, err
	}
	var records []ThemePackageRecord
	if err := json.Unmarshal(data, &records); err != nil {
		return nil, err
	}
	return records, nil
}

func (h *ThemeHandler) saveIndex(records []ThemePackageRecord) error {
	if err := os.MkdirAll(h.baseDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(h.indexPath(), data, 0o644)
}

func (h *ThemeHandler) indexPath() string {
	return filepath.Join(h.baseDir, themeIndexFile)
}

func (h *ThemeHandler) themeDir(id string) string {
	return filepath.Join(h.baseDir, id)
}

func (h *ThemeHandler) resolveThemeAssetPath(slug, filename string) (string, bool) {
	if !themeSlugPattern.MatchString(slug) {
		return "", false
	}
	return resolvePathWithin(h.themeDir(slug), filename)
}

func findThemeManifest(files []*zip.File) *zip.File {
	for _, file := range files {
		if strings.Trim(file.Name, "/") == "manifest.json" {
			return file
		}
	}
	return nil
}

func readThemeManifest(file *zip.File) (ThemeManifest, error) {
	src, err := file.Open()
	if err != nil {
		return ThemeManifest{}, fmt.Errorf("open manifest: %w", err)
	}
	defer func() { _ = src.Close() }()
	var manifest ThemeManifest
	if err := json.NewDecoder(io.LimitReader(src, 1<<20)).Decode(&manifest); err != nil {
		return ThemeManifest{}, fmt.Errorf("invalid manifest.json")
	}
	return manifest, nil
}

func validateThemeManifest(manifest ThemeManifest) error {
	if !themeSlugPattern.MatchString(manifest.ID) {
		return fmt.Errorf("theme id must be lowercase letters, numbers, underscores, or hyphens")
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return fmt.Errorf("theme name is required")
	}
	if strings.TrimSpace(manifest.Version) == "" {
		return fmt.Errorf("theme version is required")
	}
	if strings.TrimSpace(manifest.Entry) == "" || filepath.Ext(manifest.Entry) != ".css" {
		return fmt.Errorf("theme entry must be a css file")
	}
	if manifest.Sub2APIThemeAPI != "" && manifest.Sub2APIThemeAPI != "1" {
		return fmt.Errorf("unsupported theme api version")
	}
	return validateThemeRelativePath(manifest.Entry)
}

func extractThemeZip(files []*zip.File, targetDir string) error {
	var totalUnpackedSize uint64
	for _, file := range files {
		name := strings.Trim(file.Name, "/")
		if name == "" || file.FileInfo().IsDir() {
			continue
		}
		totalUnpackedSize += file.UncompressedSize64
		if totalUnpackedSize > themeMaxUploadSize {
			return fmt.Errorf("theme package is too large after extraction")
		}
		if file.FileInfo().Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("theme package cannot contain symlinks")
		}
		if err := validateThemeRelativePath(name); err != nil {
			return err
		}
		dst, ok := resolvePathWithin(targetDir, name)
		if !ok {
			return fmt.Errorf("invalid theme path")
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := extractThemeFile(file, dst); err != nil {
			return err
		}
	}
	return nil
}

func extractThemeFile(file *zip.File, dst string) error {
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	written, err := io.Copy(out, io.LimitReader(src, themeMaxUploadSize+1))
	if err != nil {
		return err
	}
	if written > themeMaxUploadSize {
		return fmt.Errorf("theme file is too large")
	}
	return nil
}

func validateThemeRelativePath(name string) error {
	if strings.Contains(name, "\\") || strings.HasPrefix(name, "/") || strings.Contains(name, "../") {
		return fmt.Errorf("invalid theme path: %s", name)
	}
	ext := strings.ToLower(filepath.Ext(name))
	if _, blocked := disallowedThemeEntries[ext]; blocked {
		return fmt.Errorf("theme package cannot contain executable frontend code: %s", name)
	}
	if _, ok := allowedThemeExtension[ext]; !ok {
		return fmt.Errorf("theme package contains unsupported file type: %s", name)
	}
	return nil
}

func validateExtractedThemeCSS(baseDir, entryPath string) error {
	return filepath.WalkDir(baseDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || strings.ToLower(filepath.Ext(path)) != ".css" {
			return nil
		}
		return validateThemeCSSFile(baseDir, entryPath, path)
	})
}

func validateThemeCSSFile(baseDir, entryPath, cssPath string) error {
	content, err := os.ReadFile(cssPath)
	if err != nil {
		return fmt.Errorf("read theme css: %w", err)
	}
	source := string(content)
	if cssImportPattern.MatchString(source) {
		return fmt.Errorf("theme css cannot use @import: %s", filepath.Base(cssPath))
	}

	matches := cssURLPattern.FindAllStringSubmatch(source, -1)
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		ref := normalizeCSSURLReference(match[1])
		if ref == "" || strings.HasPrefix(ref, "#") {
			continue
		}
		if isForbiddenThemeAssetReference(ref) {
			return fmt.Errorf("theme css cannot load external assets: %s", filepath.Base(cssPath))
		}
		resolvedAssetPath, ok := resolvePathWithin(filepath.Dir(cssPath), ref)
		if !ok {
			return fmt.Errorf("theme css references an invalid asset path: %s", filepath.Base(cssPath))
		}
		info, statErr := os.Stat(resolvedAssetPath)
		if statErr != nil || info.IsDir() {
			return fmt.Errorf("theme css references an invalid asset path: %s", filepath.Base(cssPath))
		}
	}

	if cssPath == entryPath {
		return nil
	}

	rel, err := filepath.Rel(baseDir, cssPath)
	if err != nil {
		return fmt.Errorf("validate theme css: %w", err)
	}
	if _, ok := resolvePathWithin(baseDir, rel); !ok {
		return fmt.Errorf("theme css references an invalid path: %s", filepath.Base(cssPath))
	}
	return nil
}

func normalizeCSSURLReference(value string) string {
	trimmed := strings.TrimSpace(value)
	trimmed = strings.Trim(trimmed, `"'`)
	return strings.TrimSpace(trimmed)
}

func isForbiddenThemeAssetReference(ref string) bool {
	lower := strings.ToLower(ref)
	return strings.HasPrefix(lower, "http://") ||
		strings.HasPrefix(lower, "https://") ||
		strings.HasPrefix(lower, "//") ||
		strings.HasPrefix(lower, "data:") ||
		strings.HasPrefix(lower, "javascript:") ||
		strings.HasPrefix(lower, "/")
}

func resolvePathWithin(base, name string) (string, bool) {
	clean := filepath.Clean(name)
	if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
		return "", false
	}
	baseAbs, err := filepath.Abs(base)
	if err != nil {
		return "", false
	}
	pathAbs, err := filepath.Abs(filepath.Join(baseAbs, clean))
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(baseAbs, pathAbs)
	if err != nil || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return "", false
	}
	return pathAbs, true
}

func themeAssetURL(slug, filename string) string {
	return "/api/v1/themes/assets/" + urlPathEscape(slug) + "/" + strings.TrimPrefix(filename, "/")
}

func urlPathEscape(value string) string {
	return strings.ReplaceAll(url.QueryEscape(value), "+", "%20")
}
