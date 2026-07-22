package explorer

import (
	"crypto/sha256"
	"encoding/binary"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/cloudreve/Cloudreve/v4/application/dependency"
	"github.com/cloudreve/Cloudreve/v4/inventory"
	"github.com/cloudreve/Cloudreve/v4/inventory/types"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/fs"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/manager"
	"github.com/cloudreve/Cloudreve/v4/pkg/filemanager/manager/entitysource"
	"github.com/cloudreve/Cloudreve/v4/pkg/hashid"
	"github.com/cloudreve/Cloudreve/v4/pkg/serializer"
	"github.com/gin-gonic/gin"
	"golang.org/x/tools/container/intsets"
)

// ImageExtensions defines common image file extensions
var ImageExtensions = map[string]bool{
	"jpg": true, "jpeg": true, "png": true, "gif": true,
	"webp": true, "bmp": true, "avif": true, "heic": true,
	"heif": true, "tiff": true, "tif": true, "svg": true,
	"ico": true, "raw": true,
}

// Metadata keys for image dimensions and format
var (
	imageWidthKeys  = []string{"stream:width", "image:x", "format:width"}
	imageHeightKeys = []string{"stream:height", "image:y", "format:height"}
	imageFormatKeys = []string{"stream:format", "stream:codec_name", "format:format_name"}
)

type (
	RandomFileParamCtx struct{}

	RandomFileService struct {
		Uri       string   `form:"uri" binding:"required"`
		Recursive bool     `form:"recursive"`
		Count     int      `form:"count"`
		Thumbnail bool     `form:"thumbnail"`
		Exclude   []string `form:"exclude"`
		Ext       string   `form:"ext"`
		MinSize   int64    `form:"min_size"`
		MaxSize   int64    `form:"max_size"`
		Seed      string   `form:"seed"`
	}

	RandomFileResponse struct {
		Images     []RandomImageResponse `json:"images"`
		TotalInDir int64                 `json:"total_in_dir"`
		SeedUsed   string                `json:"seed_used"`
	}

	RandomImageResponse struct {
		ID       string     `json:"id"`
		Name     string     `json:"name"`
		Path     string     `json:"path"`
		Size     int64      `json:"size"`
		Width    int        `json:"width"`
		Height   int        `json:"height"`
		Format   string     `json:"format"`
		URL      string     `json:"url"`
		ThumbURL string     `json:"thumb_url"`
		Expires  *time.Time `json:"expires"`
	}
)

// GetRandomFiles retrieves random image files from a directory
func (s *RandomFileService) GetRandomFiles(c *gin.Context) (*RandomFileResponse, error) {
	dep := dependency.FromContext(c)
	user := inventory.UserFromContext(c)
	m := manager.NewFileManager(dep, user)
	defer m.Recycle()

	// Apply defaults: recursive defaults to true if not explicitly set
	if _, ok := c.GetQuery("recursive"); !ok {
		s.Recursive = true
	}

	// Apply count bounds
	if s.Count <= 0 {
		s.Count = 1
	}
	if s.Count > 50 {
		s.Count = 50
	}

	// Parse allowed extensions from filter
	allowedExts := parseExtFilter(s.Ext)

	// Build exclude set for fast lookup
	excludeSet := make(map[string]bool, len(s.Exclude))
	for _, e := range s.Exclude {
		excludeSet[e] = true
	}

	// Parse URI
	uri, err := fs.NewUriFromString(s.Uri)
	if err != nil {
		return nil, serializer.NewError(serializer.CodeParamErr, "unknown uri", err)
	}

	// Collect all image files by walking the directory tree
	var imageFiles []fs.File
	walkDepth := 1
	if s.Recursive {
		walkDepth = intsets.MaxInt // unlimited depth
	}

	err = m.Walk(c, uri, walkDepth, func(file fs.File, level int) error {
		// Skip directories
		if file.Type() != types.FileTypeFile {
			return nil
		}

		// Filter by extension
		ext := strings.ToLower(file.Ext())
		if len(allowedExts) > 0 {
			if !allowedExts[ext] {
				return nil
			}
		} else if !ImageExtensions[ext] {
			return nil
		}

		// Filter by size
		if s.MinSize > 0 && file.Size() < s.MinSize {
			return nil
		}
		if s.MaxSize > 0 && file.Size() > s.MaxSize {
			return nil
		}

		// Filter by exclude list
		fileName := file.DisplayName()
		filePath := file.Uri(false).String()
		if excludeSet[fileName] || excludeSet[filePath] {
			return nil
		}

		imageFiles = append(imageFiles, file)
		return nil
	})
	if err != nil {
		return nil, err
	}

	totalInDir := int64(len(imageFiles))
	if totalInDir == 0 {
		return &RandomFileResponse{
			Images:     []RandomImageResponse{},
			TotalInDir: 0,
			SeedUsed:   s.Seed,
		}, nil
	}

	// Random selection with seed support
	selectedFiles := s.selectRandom(imageFiles)

	// Build response
	settings := dep.SettingProvider()
	expire := time.Now().Add(settings.EntityUrlValidDuration(c))

	images := make([]RandomImageResponse, 0, len(selectedFiles))
	for _, file := range selectedFiles {
		imgResp, err := s.buildImageResponse(c, m, file, &expire)
		if err != nil {
			// Skip files that fail to generate URL
			continue
		}
		images = append(images, *imgResp)
	}

	return &RandomFileResponse{
		Images:     images,
		TotalInDir: totalInDir,
		SeedUsed:   s.Seed,
	}, nil
}

// selectRandom selects count random files from the list, with optional seed
func (s *RandomFileService) selectRandom(files []fs.File) []fs.File {
	if len(files) <= s.Count {
		return files
	}

	var rng *rand.Rand
	if s.Seed != "" {
		// Create a deterministic seed from the provided seed + today's date
		seedSource := s.Seed + time.Now().Format("2006-01-02")
		hash := sha256.Sum256([]byte(seedSource))
		seed := int64(binary.BigEndian.Uint64(hash[:8]))
		rng = rand.New(rand.NewSource(seed))
	} else {
		rng = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	// Fisher-Yates shuffle for first count elements
	result := make([]fs.File, len(files))
	copy(result, files)
	for i := 0; i < s.Count; i++ {
		j := i + rng.Intn(len(result)-i)
		result[i], result[j] = result[j], result[i]
	}

	return result[:s.Count]
}

// buildImageResponse builds a single image response with URL and metadata
func (s *RandomFileService) buildImageResponse(c *gin.Context, m manager.FileManager, file fs.File, expire *time.Time) (*RandomImageResponse, error) {
	dep := dependency.FromContext(c)
	hasher := dep.HashIDEncoder()

	// Get direct link or thumbnail URL
	fileUri := file.Uri(false)
	urls, _, err := m.GetEntityUrls(c, []manager.GetEntityUrlArgs{
		{URI: fileUri},
	}, fs.WithUrlExpire(expire))
	if err != nil || len(urls) == 0 {
		return nil, err
	}

	resp := &RandomImageResponse{
		ID:      hashid.EncodeFileID(hasher, file.ID()),
		Name:    file.DisplayName(),
		Path:    fileUri.String(),
		Size:    file.Size(),
		URL:     urls[0].Url,
		Expires: expire,
	}

	// Get thumbnail URL if requested
	if s.Thumbnail {
		thumbSource, err := m.Thumbnail(c, fileUri)
		if err == nil {
			thumbUrl, err := thumbSource.Url(c, entitysource.WithExpire(expire))
			if err == nil {
				resp.ThumbURL = thumbUrl.Url
			}
		}
	}

	// Extract image metadata (width, height, format)
	metadata := file.Metadata()
	resp.Width, resp.Height = extractDimensions(metadata)
	resp.Format = extractFormat(metadata, file.Ext())

	return resp, nil
}

// parseExtFilter parses the comma-separated extension filter
func parseExtFilter(ext string) map[string]bool {
	if ext == "" {
		return nil
	}

	parts := strings.Split(ext, ",")
	result := make(map[string]bool, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.ToLower(p))
		// Remove leading dot if present
		p = strings.TrimPrefix(p, ".")
		if p != "" {
			result[p] = true
		}
	}
	return result
}

// extractDimensions extracts width and height from file metadata
func extractDimensions(metadata map[string]string) (width, height int) {
	for _, key := range imageWidthKeys {
		if v, ok := metadata[key]; ok {
			if w, err := strconv.Atoi(v); err == nil && w > 0 {
				width = w
				break
			}
		}
	}
	for _, key := range imageHeightKeys {
		if v, ok := metadata[key]; ok {
			if h, err := strconv.Atoi(v); err == nil && h > 0 {
				height = h
				break
			}
		}
	}
	return
}

// extractFormat extracts image format from file metadata
func extractFormat(metadata map[string]string, ext string) string {
	for _, key := range imageFormatKeys {
		if v, ok := metadata[key]; ok && v != "" {
			return strings.ToLower(v)
		}
	}

	// Fallback to extension
	if ext != "" {
		ext = strings.ToLower(ext)
		switch ext {
		case "jpg", "jpeg":
			return "jpeg"
		case "png":
			return "png"
		case "gif":
			return "gif"
		case "webp":
			return "webp"
		case "avif":
			return "avif"
		case "heic":
			return "heic"
		case "heif":
			return "heif"
		case "bmp":
			return "bmp"
		case "tiff", "tif":
			return "tiff"
		default:
			return ext
		}
	}
	return ""
}
