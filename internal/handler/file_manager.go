package handler

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
)

const uploadsBaseDir = "./uploads"

// FileEntry represents a file or directory in the uploads tree
type FileEntry struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"isDir"`
	Size     int64       `json:"size,omitempty"`
	ModTime  time.Time   `json:"modTime"`
	Children []FileEntry `json:"children,omitempty"`
}

// ListFiles returns the uploads directory tree
func ListFiles(c echo.Context) error {
	subPath := c.QueryParam("path")
	targetDir := filepath.Join(uploadsBaseDir, filepath.Clean(subPath))

	// Prevent directory traversal
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid path", "INVALID_PATH", "")
	}
	absBase, _ := filepath.Abs(uploadsBaseDir)
	if absTarget != absBase && !strings.HasPrefix(absTarget, absBase+string(os.PathSeparator)) {
		return ErrorResponse(c, http.StatusForbidden, "Access denied", "TRAVERSAL", "")
	}

	entries, err := readDir(targetDir, subPath)
	if err != nil {
		return ErrorResponse(c, http.StatusNotFound, "Directory not found", "NOT_FOUND", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "Files listed", entries)
}

// DeleteFile deletes a file from uploads (admin only)
func DeleteFile(c echo.Context) error {
	filePath := c.QueryParam("path")
	if filePath == "" {
		return ErrorResponse(c, http.StatusBadRequest, "File path is required", "MISSING_PATH", "")
	}

	targetPath := filepath.Join(uploadsBaseDir, filepath.Clean(filePath))

	// Prevent directory traversal
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return ErrorResponse(c, http.StatusBadRequest, "Invalid path", "INVALID_PATH", "")
	}
	absBase, _ := filepath.Abs(uploadsBaseDir)
	if absTarget == absBase || !strings.HasPrefix(absTarget, absBase+string(os.PathSeparator)) {
		return ErrorResponse(c, http.StatusForbidden, "Access denied", "TRAVERSAL", "")
	}

	if err := os.Remove(targetPath); err != nil {
		return ErrorResponse(c, http.StatusNotFound, "File not found", "NOT_FOUND", err.Error())
	}

	return SuccessResponse(c, http.StatusOK, "File deleted", map[string]string{"path": filePath})
}

func readDir(dirPath, relativePath string) ([]FileEntry, error) {
	dirEntries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, err
	}

	var entries []FileEntry
	for _, de := range dirEntries {
		info, err := de.Info()
		if err != nil {
			continue
		}

		entryPath := relativePath
		if entryPath != "" {
			entryPath = entryPath + "/" + de.Name()
		} else {
			entryPath = de.Name()
		}

		entry := FileEntry{
			Name:    de.Name(),
			Path:    entryPath,
			IsDir:   de.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}

		entries = append(entries, entry)
	}

	if entries == nil {
		entries = []FileEntry{}
	}

	return entries, nil
}
