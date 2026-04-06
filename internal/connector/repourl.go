package connector

import (
	"net/url"
	"strings"
)

// ParseRepoURL extracts the owner and repo name from a Git repository URL.
// Supports HTTPS URLs (https://github.com/org/repo.git) and SSH URLs (git@github.com:org/repo.git).
func ParseRepoURL(repoURL string) (owner, repo string, ok bool) {
	// Handle SSH URLs: git@github.com:org/repo.git
	if strings.HasPrefix(repoURL, "git@") || strings.HasPrefix(repoURL, "ssh://") {
		// Strip ssh:// prefix if present
		s := repoURL
		if strings.HasPrefix(s, "ssh://") {
			s = strings.TrimPrefix(s, "ssh://")
		}
		// git@github.com:org/repo.git -> org/repo.git
		idx := strings.Index(s, ":")
		if idx < 0 {
			return "", "", false
		}
		path := s[idx+1:]
		path = strings.TrimSuffix(path, ".git")
		parts := strings.SplitN(path, "/", 3)
		if len(parts) < 2 {
			return "", "", false
		}
		return parts[0], parts[1], true
	}

	// Handle HTTPS URLs
	u, err := url.Parse(repoURL)
	if err != nil {
		return "", "", false
	}

	path := strings.TrimPrefix(u.Path, "/")
	path = strings.TrimSuffix(path, ".git")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}
