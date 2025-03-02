package box

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	zlogger "github.com/0chain/s3migration/logger"
	T "github.com/0chain/s3migration/types"
)

const (
	apiBaseUrl = "https://api.box.com/2.0"
)

var (
	transport = &http.Transport{
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
		DisableCompression:    false,
		MaxIdleConnsPerHost:   10,
		ReadBufferSize:        64 * 1024, // 64KB buffer
		WriteBufferSize:       64 * 1024, // 64KB buffer
		ForceAttemptHTTP2:     false,
	}
)

type BoxClient struct {
	ClientID     string
	ClientSecret string
	AccessToken  string
	RefreshToken string
	NewerThan    *time.Time
	OlderThan    *time.Time
	WorkDir      string
}

func GetBoxClient(options BoxClient) (*BoxClient, error) {

	err := checkTokenValid(context.Background(), &options)
	if err != nil {
		return &options, err
	}

	return &options, nil
}

func checkTokenValid(ctx context.Context, client *BoxClient) error {
	// Simple validation check - try to make a lightweight API call
	url := fmt.Sprintf("%s/users/me", apiBaseUrl)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", client.AccessToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	// If token is invalid (401), try to refresh it
	if resp.StatusCode == http.StatusUnauthorized {
		return RefreshToken(ctx, client)
	}

	return nil
}

func RefreshToken(ctx context.Context, client *BoxClient) error {
	baseUrl := "https://api.box.com/oauth2/token"
	data := make(url.Values)
	data.Set("grant_type", "refresh_token")
	data.Set("refresh_token", client.RefreshToken)
	data.Set("client_id", client.ClientID)
	data.Set("client_secret", client.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, "POST", baseUrl, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to c	reate refresh token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("failed to execute refresh token request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("failed to refresh token: status %s, response: %s", resp.Status, string(body))
	}

	var tokenResponse struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
		return fmt.Errorf("failed to decode token response: %w", err)
	}

	// Update client with new tokens
	client.AccessToken = tokenResponse.AccessToken
	client.RefreshToken = tokenResponse.RefreshToken

	return nil
}

func (d *BoxClient) ListFiles(ctx context.Context) (<-chan *T.ObjectMeta, <-chan error) {
	objectChan := make(chan *T.ObjectMeta, 100)
	errChan := make(chan error, 10)

	go func() {
		defer func() {
			close(objectChan)
			close(errChan)
		}()

		folderID := "0" // root folder
		limit := 100
		offset := 0

		for {
			select {
			case <-ctx.Done():
				errChan <- ctx.Err()
				return
			default:
				// Continue processing
			}

			url := fmt.Sprintf("%s/folders/%s/items?limit=%d&offset=%d&fields=id,name,size,modified_at,extension,mime_type",
				apiBaseUrl, folderID, limit, offset)

			req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
			if err != nil {
				errChan <- err
				return
			}

			req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", d.AccessToken))

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				errChan <- err
				return
			}

			// if token is expired, refresh it
			if resp.StatusCode == http.StatusUnauthorized {
				err := RefreshToken(ctx, d)
				if err != nil {
					errChan <- fmt.Errorf("failed to refresh token: %w", err)
					return
				}

				// Retry the request with the new access token
				req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", d.AccessToken))
				resp, err = http.DefaultClient.Do(req)
				if err != nil {
					errChan <- err
					return
				}
			}

			if resp.StatusCode != http.StatusOK {
				errMsg := fmt.Sprintf("failed to list files: %s", resp.Status)
				resp.Body.Close()
				errChan <- fmt.Errorf(errMsg)
				return
			}

			var result struct {
				TotalCount int `json:"total_count"`
				Entries    []struct {
					ID         string `json:"id"`
					Name       string `json:"name"`
					Size       int64  `json:"size"`
					ModifiedAt string `json:"modified_at"`
					Extension  string `json:"extension,omitempty"`
					MimeType   string `json:"mime_type,omitempty"`
				} `json:"entries"`
				Offset int `json:"offset"`
				Limit  int `json:"limit"`
			}

			err = json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()
			if err != nil {
				errChan <- err
				return
			}

			for _, item := range result.Entries {
				lastModified, err := time.Parse(time.RFC3339, item.ModifiedAt)
				if err != nil {
					zlogger.Logger.Error(err)
					continue
				}

				// Apply date filtering
				if (d.NewerThan == nil || d.NewerThan.Unix() == 0 || lastModified.Unix() >= d.NewerThan.Unix()) &&
					(d.OlderThan == nil || d.OlderThan.Unix() == 0 || lastModified.Unix() <= d.OlderThan.Unix()) {

					// Use non-blocking send with timeout to prevent hanging
					select {
					case objectChan <- &T.ObjectMeta{
						Key:         item.ID,
						Size:        item.Size,
						ContentType: item.MimeType,
						Ext:         item.Extension,
						Name:        &item.Name,
					}:
					case <-ctx.Done():
						errChan <- ctx.Err()
						return
					}
				}
			}

			if len(result.Entries) == 0 || len(result.Entries) < limit {
				break
			}
			offset += len(result.Entries)
		}
	}()

	return objectChan, errChan
}

func (d *BoxClient) GetFileContent(ctx context.Context, fileID string) (*T.Object, error) {
	url := fmt.Sprintf("%s/files/%s/content", apiBaseUrl, fileID)
	client := &http.Client{
		Timeout:   5 * time.Minute,
		Transport: transport,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", d.AccessToken))
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		err := RefreshToken(ctx, d)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh token: %w", err)
		}

		// Retry the request with the new access token
		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", d.AccessToken))
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to execute request: %w", err)
		}
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("failed to download file: status %s", resp.Status)
	}

	content, err := io.ReadAll(resp.Body)
	if err != nil {
		resp.Body.Close()
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}
	resp.Body.Close()

	bodyReader := io.NopCloser(bytes.NewReader(content))

	obj := &T.Object{
		Body:          bodyReader,
		ContentType:   resp.Header.Get("Content-Type"),
		ContentLength: resp.ContentLength,
	}

	return obj, nil
}

func (d *BoxClient) DownloadToMemory(ctx context.Context, fileID string, offset int64, chunkSize, fileSize int64) ([]byte, error) {
	limit := offset + chunkSize - 1
	if limit > fileSize {
		limit = fileSize
	}

	rng := fmt.Sprintf("bytes=%d-%d", offset, limit)

	url := fmt.Sprintf("%s/files/%s/content", apiBaseUrl, fileID)

	client := &http.Client{
		Transport: transport,
		Timeout:   2 * time.Minute,
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", d.AccessToken))

	req.Header.Set("Range", rng)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		err := RefreshToken(ctx, d)
		if err != nil {
			return nil, fmt.Errorf("failed to refresh token: %w", err)
		}

		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", d.AccessToken))
		resp, err = client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("failed to execute request: %w", err)
		}
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return nil, fmt.Errorf("failed to download file: status %s", resp.Status)
	}

	data, err := io.ReadAll(io.LimitReader(resp.Body, chunkSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	expectedSize := limit - offset + 1
	if int64(len(data)) < expectedSize && offset+int64(len(data)) < fileSize {
		return nil, fmt.Errorf("incomplete read: got %d bytes, expected %d bytes", len(data), expectedSize)
	}

	return data, nil
}

func (d *BoxClient) DownloadToFile(ctx context.Context, fileID string) (string, error) {
	url := fmt.Sprintf("%s/files/%s/content", apiBaseUrl, fileID)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", d.AccessToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		err := RefreshToken(ctx, d)
		if err != nil {
			return "", fmt.Errorf("failed to refresh token: %w", err)
		}

		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", d.AccessToken))
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("failed to execute request: %w", err)
		}
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("failed to download file: %s", resp.Status)
	}

	filename := fileID
	if contentDisposition := resp.Header.Get("Content-Disposition"); contentDisposition != "" {
		if _, params, err := mime.ParseMediaType(contentDisposition); err == nil {
			if fn, ok := params["filename"]; ok && fn != "" {
				filename = fn
			}
		}
	}

	file, err := os.Create(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	_, err = io.Copy(file, resp.Body)
	if err != nil {
		os.Remove(filename)
		return "", err
	}

	err = file.Sync()
	if err != nil {
		return "", err
	}

	return filename, nil
}

func (d *BoxClient) DeleteFile(ctx context.Context, fileID string) error {
	url := fmt.Sprintf("%s/files/%s", apiBaseUrl, fileID)
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", d.AccessToken))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode == http.StatusUnauthorized {
		err := RefreshToken(ctx, d)
		if err != nil {
			return fmt.Errorf("failed to refresh token: %w", err)
		}

		req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", d.AccessToken))
		resp, err = http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("failed to execute request: %w", err)
		}
	}
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("failed to delete file: %s", resp.Status)
	}

	return nil
}
