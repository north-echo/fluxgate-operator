package analyzer

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	fluxgatemetrics "github.com/north-echo/fluxgate-operator/internal/metrics"
)

// WorkflowFile represents a fetched workflow file from a repository.
type WorkflowFile struct {
	Name    string
	Content []byte
}

// GitHubClient fetches workflow files from GitHub repositories using the Contents API.
type GitHubClient struct {
	Token      string
	HTTPClient *http.Client
	BaseURL    string // defaults to "https://api.github.com"
}

// githubContentEntry represents a file entry from the GitHub Contents API.
type githubContentEntry struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Type        string `json:"type"` // "file" or "dir"
	DownloadURL string `json:"download_url"`
	Content     string `json:"content"`
	Encoding    string `json:"encoding"`
}

// FetchWorkflowFiles lists and fetches all .yml/.yaml files from .github/workflows/.
func (g *GitHubClient) FetchWorkflowFiles(ctx context.Context, owner, repo, branch string) ([]WorkflowFile, error) {
	baseURL := g.BaseURL
	if baseURL == "" {
		baseURL = "https://api.github.com"
	}

	httpClient := g.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	// List contents of .github/workflows/
	listURL := fmt.Sprintf("%s/repos/%s/%s/contents/.github/workflows?ref=%s",
		baseURL, owner, repo, branch)

	entries, err := g.fetchContents(ctx, httpClient, listURL)
	if err != nil {
		return nil, fmt.Errorf("listing workflow directory: %w", err)
	}

	var files []WorkflowFile
	for _, entry := range entries {
		if entry.Type != "file" {
			continue
		}
		if !strings.HasSuffix(entry.Name, ".yml") && !strings.HasSuffix(entry.Name, ".yaml") {
			continue
		}

		// Fetch individual file content
		fileURL := fmt.Sprintf("%s/repos/%s/%s/contents/%s?ref=%s",
			baseURL, owner, repo, entry.Path, branch)

		content, err := g.fetchFileContent(ctx, httpClient, fileURL)
		if err != nil {
			continue // skip files we can't fetch
		}

		files = append(files, WorkflowFile{
			Name:    entry.Name,
			Content: content,
		})
	}

	return files, nil
}

// fetchContents fetches a directory listing from the GitHub Contents API.
func (g *GitHubClient) fetchContents(ctx context.Context, httpClient *http.Client, url string) ([]githubContentEntry, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	g.setHeaders(req)

	resp, err := httpClient.Do(req)
	if err != nil {
		fluxgatemetrics.GitHubAPIRequests.WithLabelValues("contents", "error").Inc()
		return nil, err
	}
	defer resp.Body.Close()

	// Record API metrics
	fluxgatemetrics.GitHubAPIRequests.WithLabelValues("contents", strconv.Itoa(resp.StatusCode)).Inc()
	g.recordRateLimit(resp)

	if resp.StatusCode == http.StatusNotFound {
		return nil, nil // no workflows directory
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("GitHub API returned %d: %s", resp.StatusCode, string(body))
	}

	var entries []githubContentEntry
	if err := json.NewDecoder(resp.Body).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decoding contents response: %w", err)
	}
	return entries, nil
}

// fetchFileContent fetches a single file's content from the GitHub Contents API.
func (g *GitHubClient) fetchFileContent(ctx context.Context, httpClient *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	g.setHeaders(req)

	resp, err := httpClient.Do(req)
	if err != nil {
		fluxgatemetrics.GitHubAPIRequests.WithLabelValues("contents/file", "error").Inc()
		return nil, err
	}
	defer resp.Body.Close()

	// Record API metrics
	fluxgatemetrics.GitHubAPIRequests.WithLabelValues("contents/file", strconv.Itoa(resp.StatusCode)).Inc()
	g.recordRateLimit(resp)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var entry githubContentEntry
	if err := json.NewDecoder(resp.Body).Decode(&entry); err != nil {
		return nil, fmt.Errorf("decoding file response: %w", err)
	}

	if entry.Encoding == "base64" {
		// GitHub returns base64-encoded content with possible newlines
		cleaned := strings.ReplaceAll(entry.Content, "\n", "")
		return base64.StdEncoding.DecodeString(cleaned)
	}

	return []byte(entry.Content), nil
}

// recordRateLimit extracts the GitHub rate limit remaining header and updates the metric.
func (g *GitHubClient) recordRateLimit(resp *http.Response) {
	if remaining := resp.Header.Get("X-RateLimit-Remaining"); remaining != "" {
		if val, err := strconv.ParseFloat(remaining, 64); err == nil {
			fluxgatemetrics.GitHubAPIRateRemaining.Set(val)
		}
	}
}

// setHeaders sets common headers for GitHub API requests.
func (g *GitHubClient) setHeaders(req *http.Request) {
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "fluxgate-operator/1.0")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
}
