package helper

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
)

// maxDownloadSize mirrors WhatsApp's ~100MB document limit.
const maxDownloadSize = 100 * 1024 * 1024

// DetectMediaType detects media type from filename extension
func DetectMediaType(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))

	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".webp":
		return "image"
	case ".mp4", ".mov", ".avi", ".mkv":
		return "video"
	case ".mp3", ".ogg", ".m4a", ".opus":
		return "audio"
	case ".pdf", ".doc", ".docx", ".xls", ".xlsx", ".txt", ".zip":
		return "document"
	default:
		return "document"
	}
}

// DetectMediaTypeFromBytes returns the high-level media category inferred from
// content sniffing (http.DetectContentType over the first 512 bytes). Callers
// should use this as the authoritative category instead of the client-supplied
// filename extension.
func DetectMediaTypeFromBytes(data []byte) string {
	sniffed := http.DetectContentType(data)
	switch {
	case strings.HasPrefix(sniffed, "image/"):
		return "image"
	case strings.HasPrefix(sniffed, "video/"):
		return "video"
	case strings.HasPrefix(sniffed, "audio/"):
		return "audio"
	default:
		return "document"
	}
}

// imageMimeTypes maps an image extension to its MIME type.
var imageMimeTypes = map[string]string{
	".jpg":  "image/jpeg",
	".jpeg": "image/jpeg",
	".png":  "image/png",
	".gif":  "image/gif",
	".webp": "image/webp",
}

// documentMimeTypes maps a document extension to its MIME type.
var documentMimeTypes = map[string]string{
	".pdf":  "application/pdf",
	".doc":  "application/msword",
	".docx": "application/msword",
	".xls":  "application/vnd.ms-excel",
	".xlsx": "application/vnd.ms-excel",
	".zip":  "application/zip",
}

// lookupMimeType returns the mapped MIME type, or fallback for an unknown
// extension.
func lookupMimeType(table map[string]string, ext, fallback string) string {
	if mime, ok := table[ext]; ok {
		return mime
	}
	return fallback
}

// GetMimeType returns MIME type based on media type
func GetMimeType(mediaType, filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))

	switch mediaType {
	case "image":
		return lookupMimeType(imageMimeTypes, ext, "image/jpeg")
	case "video":
		return "video/mp4"
	case "audio":
		return "audio/mpeg"
	default: // document
		return lookupMimeType(documentMimeTypes, ext, "application/octet-stream")
	}
}

// CreateMediaMessage creates WhatsApp media message based on type
func CreateMediaMessage(uploaded whatsmeow.UploadResponse, caption, filename, mediaType string) *waE2E.Message {
	msg := &waE2E.Message{}
	mimeType := GetMimeType(mediaType, filename)

	switch mediaType {
	case "image":
		msg.ImageMessage = &waE2E.ImageMessage{
			Caption:       &caption,
			URL:           &uploaded.URL,
			DirectPath:    &uploaded.DirectPath,
			MediaKey:      uploaded.MediaKey,
			Mimetype:      &mimeType,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    &uploaded.FileLength,
		}
	case "video":
		msg.VideoMessage = &waE2E.VideoMessage{
			Caption:       &caption,
			URL:           &uploaded.URL,
			DirectPath:    &uploaded.DirectPath,
			MediaKey:      uploaded.MediaKey,
			Mimetype:      &mimeType,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    &uploaded.FileLength,
		}
	case "audio":
		msg.AudioMessage = &waE2E.AudioMessage{
			URL:           &uploaded.URL,
			DirectPath:    &uploaded.DirectPath,
			MediaKey:      uploaded.MediaKey,
			Mimetype:      &mimeType,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    &uploaded.FileLength,
		}
	default: // document
		msg.DocumentMessage = &waE2E.DocumentMessage{
			Caption:       &caption,
			URL:           &uploaded.URL,
			DirectPath:    &uploaded.DirectPath,
			MediaKey:      uploaded.MediaKey,
			Mimetype:      &mimeType,
			FileEncSHA256: uploaded.FileEncSHA256,
			FileSHA256:    uploaded.FileSHA256,
			FileLength:    &uploaded.FileLength,
			FileName:      &filename,
		}
	}

	return msg
}

// mediaDownloadClient is a package-level SSRF-safe HTTP client reused across
// all download calls. Sharing the client keeps TCP/TLS connection pools warm
// and avoids per-request Transport allocations.
var mediaDownloadClient = &http.Client{
	Timeout: 30 * time.Second,
	Transport: &http.Transport{
		DialContext: SSRFSafeDialContext,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if err := ValidateExternalURL(req.URL.String()); err != nil {
			return fmt.Errorf("redirect to blocked URL: %v", err)
		}
		return nil
	},
}

// newDownloadRequest builds the GET request used for external media downloads.
// The browser-like headers exist because several CDNs answer 403 without them.
func newDownloadRequest(url string) (*http.Request, error) {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %v", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/pdf,image/*,video/*,audio/*,*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Accept-Encoding", "gzip, deflate")
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Referer", url)
	return req, nil
}

// readDownloadBody streams the response body with a hard size ceiling. It never
// buffers more than maxDownloadSize+1 bytes of an untrusted response.
func readDownloadBody(resp *http.Response) ([]byte, error) {
	if resp.ContentLength > maxDownloadSize {
		return nil, fmt.Errorf("file too large: %d bytes (max %d)", resp.ContentLength, maxDownloadSize)
	}

	buf := &bytes.Buffer{}
	n, err := io.Copy(buf, io.LimitReader(resp.Body, maxDownloadSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %v", err)
	}
	if n > maxDownloadSize {
		return nil, fmt.Errorf("file too large: exceeds %d bytes", maxDownloadSize)
	}
	if n == 0 {
		return nil, fmt.Errorf("downloaded file is empty")
	}
	return buf.Bytes(), nil
}

// filenameFromContentDisposition extracts the filename parameter, or "" when
// the header carries none.
func filenameFromContentDisposition(header string) string {
	_, after, found := strings.Cut(header, "filename=")
	if !found {
		return ""
	}
	return strings.Trim(after, "\"")
}

// downloadFilename resolves the saved name from the Content-Disposition header,
// then the URL path, and finally the Content-Type extension.
func downloadFilename(resp *http.Response, url string) string {
	filename := filenameFromContentDisposition(resp.Header.Get("Content-Disposition"))

	if filename == "" {
		filename, _, _ = strings.Cut(filepath.Base(url), "?")
	}

	if filename == "." || filename == "/" || filename == "" {
		return "document" + getExtensionFromContentType(resp.Header.Get("Content-Type"))
	}
	return filename
}

// DownloadFile downloads file from URL and returns data and filename
func DownloadFile(url string) ([]byte, string, error) {
	// Validate URL to prevent SSRF attacks
	if err := ValidateExternalURL(url); err != nil {
		return nil, "", fmt.Errorf("URL validation failed: %v", err)
	}

	req, err := newDownloadRequest(url)
	if err != nil {
		return nil, "", err
	}

	resp, err := mediaDownloadClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("failed to download: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("failed to download: status %d (%s)", resp.StatusCode, resp.Status)
	}

	data, err := readDownloadBody(resp)
	if err != nil {
		return nil, "", err
	}

	return data, downloadFilename(resp, url), nil
}

// Helper to get file extension from Content-Type
func getExtensionFromContentType(contentType string) string {
	contentType = strings.ToLower(strings.Split(contentType, ";")[0])

	switch contentType {
	case "application/pdf":
		return ".pdf"
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "video/mp4":
		return ".mp4"
	case "audio/mpeg":
		return ".mp3"
	case "application/zip":
		return ".zip"
	default:
		return ".bin"
	}
}
