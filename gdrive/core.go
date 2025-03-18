package gdrive

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	zlogger "github.com/0chain/s3migration/logger"
	T "github.com/0chain/s3migration/types"
	"github.com/pkg/errors"
	"golang.org/x/oauth2"
	"google.golang.org/api/drive/v3"
	"google.golang.org/api/option"
)

type GoogleDriveClient struct {
	service   *drive.Service
	workDir   string
	newerThan *time.Time
	olderThan *time.Time
}

func NewGoogleDriveClient(cfg oauth2.Config, token *oauth2.Token, workDir string, newerThan *time.Time, olderThan *time.Time) (*GoogleDriveClient, error) {
	ctx := context.Background()
	var httpClient *http.Client

	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
		})
		httpClient = oauth2.NewClient(ctx, tokenSource)
	} else {
		httpClient = cfg.Client(ctx, token)
	}

	// if access token is expired, refresh it
	if token.Expiry.Before(time.Now()) {
		token, err := cfg.TokenSource(ctx, token).Token()
		if err != nil {
			return nil, err
		}
		tokenSource := oauth2.StaticTokenSource(&oauth2.Token{
			AccessToken:  token.AccessToken,
			RefreshToken: token.RefreshToken,
		})
		httpClient = oauth2.NewClient(ctx, tokenSource)
	}

	service, err := drive.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}

	// Test connection
	_, err = service.Files.List().Do()
	if err != nil {
		return nil, errors.Wrap(err, "invalid Google Drive access token")
	}

	return &GoogleDriveClient{
		service:   service,
		workDir:   workDir,
		newerThan: newerThan,
		olderThan: olderThan,
	}, nil
}

// updateFileInfo updates extension and filename based on MIME type
func updateFileInfo(file *drive.File) (string, string) {
	ext := file.FileExtension
	fileName := file.Name

	// Handle Google Workspace files that need to be exported
	switch file.MimeType {
	case "application/vnd.google-apps.folder":
		ext = "d"
	case "application/vnd.google-apps.document":
		ext = "txt"
		if !strings.HasSuffix(fileName, ".txt") {
			fileName += ".txt"
		}
	case "application/vnd.google-apps.spreadsheet":
		ext = "csv"
		if !strings.HasSuffix(fileName, ".csv") {
			fileName += ".csv"
		}
	case "application/vnd.google-apps.presentation":
		ext = "txt"
		if !strings.HasSuffix(fileName, ".txt") {
			fileName += ".txt"
		}
	case "application/vnd.google-apps.drawing":
		ext = "png"
		if !strings.HasSuffix(fileName, ".png") {
			fileName += ".png"
		}
	case "application/vnd.google-apps.script":
		ext = "json"
		if !strings.HasSuffix(fileName, ".json") {
			fileName += ".json"
		}
	}

	return ext, fileName
}

func (g *GoogleDriveClient) ListFiles(ctx context.Context) (<-chan *T.ObjectMeta, <-chan error) {
	objectChan := make(chan *T.ObjectMeta)
	errChan := make(chan error)

	go func() {
		defer func() {
			close(objectChan)
			close(errChan)
		}()

		filesReq := g.service.Files.List().Context(ctx).
			Q("trashed=false").
			Fields("nextPageToken, files(id, mimeType, quotaBytesUsed, fileExtension, name, modifiedTime)").
			PageSize(100)

		processFiles := func(files []*drive.File) {
			for _, file := range files {
				lastModified, err := time.Parse(time.RFC3339, file.ModifiedTime)
				if err != nil {
					zlogger.Logger.Error(err)
					continue
				}

				if (g.newerThan == nil || g.newerThan.Unix() == 0 || lastModified.Unix() >= g.newerThan.Unix()) &&
					(g.olderThan == nil || g.olderThan.Unix() == 0 || lastModified.Unix() <= g.olderThan.Unix()) {

					ext, fileName := updateFileInfo(file)

					size := file.QuotaBytesUsed
					contentType := file.MimeType

					if file.MimeType == "application/vnd.google-apps.folder" {
						contentType = "d"
						size = 0
						zlogger.Logger.Info(fmt.Sprintf("Folder detected: %s, setting contentType to 'd' and size to 0", fileName))
						return
					} else if isGoogleDocsFile(file.MimeType) {
						size = 0
						zlogger.Logger.Info(fmt.Sprintf("Google Docs file detected: %s, setting size to 0", fileName))
					}

					objectChan <- &T.ObjectMeta{
						Key:         file.Id,
						Size:        size,
						ContentType: contentType,
						Ext:         ext,
						Name:        &fileName,
					}
				}
			}
		}

		// Get first page
		files, err := filesReq.Do()
		if err != nil {
			errChan <- err
			return
		}

		processFiles(files.Files)

		// Continue with next pages if any
		nextPageToken := files.NextPageToken
		for nextPageToken != "" {
			files, err := filesReq.PageToken(nextPageToken).Do()
			if err != nil {
				errChan <- err
				return
			}

			processFiles(files.Files)
			nextPageToken = files.NextPageToken
		}
	}()

	return objectChan, errChan
}

func (g *GoogleDriveClient) GetFileContent(ctx context.Context, fileID string) (*T.Object, error) {
	// Get file metadata first to check if it needs to be exported
	file, err := g.service.Files.Get(fileID).Fields("mimeType").Do()
	if err != nil {
		return nil, err
	}

	var resp *http.Response
	// Handle Google Workspace files that need to be exported
	switch file.MimeType {
	case "application/vnd.google-apps.document":
		resp, err = g.service.Files.Export(fileID, "text/plain").Download()
	case "application/vnd.google-apps.spreadsheet":
		resp, err = g.service.Files.Export(fileID, "text/csv").Download()
	case "application/vnd.google-apps.presentation":
		resp, err = g.service.Files.Export(fileID, "text/plain").Download()
	case "application/vnd.google-apps.drawing":
		resp, err = g.service.Files.Export(fileID, "image/png").Download()
	case "application/vnd.google-apps.script":
		resp, err = g.service.Files.Export(fileID, "application/json").Download()
	default:
		// Regular file download
		resp, err = g.service.Files.Get(fileID).Download()
	}

	if err != nil {
		return nil, err
	}

	obj := &T.Object{
		Body:          resp.Body,
		ContentType:   resp.Header.Get("Content-Type"),
		ContentLength: resp.ContentLength,
	}

	return obj, nil
}

func (g *GoogleDriveClient) DeleteFile(ctx context.Context, fileID string) error {
	return g.service.Files.Delete(fileID).Do()
}

func (g *GoogleDriveClient) DownloadToFile(ctx context.Context, fileID string) (string, error) {
	file, err := g.service.Files.Get(fileID).Fields("name, mimeType").Do()
	if err != nil {
		return "", errors.Wrap(err, "failed to get file metadata")
	}

	if file.MimeType == "application/vnd.google-apps.folder" {
		fileName := file.Name
		fileName = strings.ReplaceAll(fileName, "/", "_")
		fileName = strings.ReplaceAll(fileName, "\\", "_")

		folderPath := path.Join(g.workDir, fileName)

		if err := os.MkdirAll(folderPath, 0755); err != nil {
			return "", errors.Wrap(err, "failed to create folder directory")
		}

		placeholderPath := path.Join(folderPath, ".folder")
		placeholder, err := os.Create(placeholderPath)
		if err != nil {
			return "", errors.Wrap(err, "failed to create folder placeholder")
		}
		defer placeholder.Close()

		_, err = placeholder.WriteString(fmt.Sprintf("Google Drive Folder: %s\nID: %s\n", file.Name, fileID))
		if err != nil {
			return "", errors.Wrap(err, "failed to write folder metadata")
		}

		zlogger.Logger.Info(fmt.Sprintf("Created folder: %s", folderPath))
		return folderPath, nil
	}

	var resp *http.Response
	fileName := file.Name

	fileName = strings.ReplaceAll(fileName, "/", "_")
	fileName = strings.ReplaceAll(fileName, "\\", "_")
	zlogger.Logger.Info("file.MimeType", file.MimeType)

	switch file.MimeType {
	case "application/vnd.google-apps.document":
		resp, err = g.service.Files.Export(fileID, "text/plain").Download()
		if !strings.HasSuffix(fileName, ".txt") {
			fileName += ".txt"
		}
	case "application/vnd.google-apps.spreadsheet":
		resp, err = g.service.Files.Export(fileID, "text/csv").Download()
		if !strings.HasSuffix(fileName, ".csv") {
			fileName += ".csv"
		}
	case "application/vnd.google-apps.presentation":
		resp, err = g.service.Files.Export(fileID, "text/plain").Download()
		if !strings.HasSuffix(fileName, ".txt") {
			fileName += ".txt"
		}
	case "application/vnd.google-apps.drawing":
		resp, err = g.service.Files.Export(fileID, "image/png").Download()
		if !strings.HasSuffix(fileName, ".png") {
			fileName += ".png"
		}
	case "application/vnd.google-apps.script":
		resp, err = g.service.Files.Export(fileID, "application/json").Download()
		if !strings.HasSuffix(fileName, ".json") {
			fileName += ".json"
		}
	default:
		resp, err = g.service.Files.Get(fileID).Download()
	}

	if err != nil {
		return "", errors.Wrap(err, "failed to download file")
	}
	defer resp.Body.Close()

	zlogger.Logger.Info(fmt.Sprintf("Original File Name: %s", fileName))
	destinationPath := path.Join(g.workDir, fileName)

	if err := os.MkdirAll(g.workDir, 0755); err != nil {
		return "", errors.Wrap(err, "failed to create work directory")
	}

	out, err := os.Create(destinationPath)
	if err != nil {
		return "", errors.Wrap(err, "failed to create output file")
	}
	defer func() {
		if cerr := out.Close(); cerr != nil && err == nil {
			err = errors.Wrap(cerr, "failed to close output file")
		}
	}()

	written, err := io.Copy(out, resp.Body)
	if err != nil {
		os.Remove(destinationPath)
		return "", errors.Wrap(err, "failed to write to output file")
	}

	zlogger.Logger.Info(fmt.Sprintf("Downloaded file ID: %s to %s (%d bytes)", fileID, destinationPath, written))
	return destinationPath, nil
}

func (g *GoogleDriveClient) DownloadToMemory(ctx context.Context, fileID string, offset int64, chunkSize, fileSize int64) ([]byte, error) {
	file, err := g.service.Files.Get(fileID).Fields("mimeType, name").Do()
	if err != nil {
		return nil, fmt.Errorf("unable to get file metadata: %v", err)
	}

	if file.MimeType == "application/vnd.google-apps.folder" {
		zlogger.Logger.Info(fmt.Sprintf("Folder detected: %s, downloading as placeholder", file.Name))
		folderPath, err := g.DownloadToFile(ctx, fileID)
		if err != nil {
			return nil, fmt.Errorf("unable to process folder %s: %v", file.Name, err)
		}
		message := fmt.Sprintf("FOLDER:%s", folderPath)
		return []byte(message), nil
	}

	if isGoogleDocsFile(file.MimeType) {
		zlogger.Logger.Info(fmt.Sprintf("Google Docs file (%s) detected in DownloadToMemory, downloading to file", file.Name))
		filePath, err := g.DownloadToFile(ctx, fileID)
		if err != nil {
			return nil, fmt.Errorf("unable to export Google Docs file %s: %v", file.Name, err)
		}

		message := fmt.Sprintf("FILE_DOWNLOADED:%s", filePath)
		return []byte(message), nil
	}

	limit := offset + chunkSize - 1
	if limit > fileSize {
		limit = fileSize
	}

	rng := fmt.Sprintf("bytes=%d-%d", offset, limit)

	req := g.service.Files.Get(fileID)
	req.Header().Set("Range", rng)

	resp, err := req.Download()
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	data := make([]byte, chunkSize)
	n, err := io.ReadFull(resp.Body, data)
	if err != nil && err != io.ErrUnexpectedEOF {
		return nil, err
	}

	// Adjust the buffer if the file size is smaller than the chunk size
	if int64(n) < chunkSize && fileSize != chunkSize {
		data = data[:n]
	}

	return data, nil
}

func isGoogleDocsFile(mimeType string) bool {
	if mimeType == "application/vnd.google-apps.folder" {
		return false
	}

	return strings.HasPrefix(mimeType, "application/vnd.google-apps.")
}

func getExportMimeType(mimeType string) string {
	switch mimeType {
	case "application/vnd.google-apps.document":
		return "text/plain" // Export Google Docs as plain text
	case "application/vnd.google-apps.spreadsheet":
		return "text/csv" // Export Google Sheets as CSV
	case "application/vnd.google-apps.presentation":
		return "text/plain" // Export Google Slides as plain text
	case "application/vnd.google-apps.drawing":
		return "image/png" // PNG is already appropriate for viewing
	case "application/vnd.google-apps.script":
		return "application/json" // JSON is already readable
	default:
		return ""
	}
}

func ShouldDownloadToFile(mimeType string) bool {
	if isGoogleDocsFile(mimeType) {
		return true
	}

	complexBinaryTypes := []string{
		"application/pdf",
		"application/vnd.openxmlformats",
		"application/vnd.ms-",
		"application/zip",
		"application/x-zip",
	}

	for _, t := range complexBinaryTypes {
		if strings.Contains(mimeType, t) {
			return true
		}
	}

	return false
}

func IsFolder(mimeType string) bool {
	return mimeType == "application/vnd.google-apps.folder"
}
