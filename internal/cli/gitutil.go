// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// detectRepoURL discovers the repository web URL and root path from git.
// Returns (repoURL, repoRoot) where repoURL ends with "/" and includes the
// branch path (e.g. "https://github.com/user/repo/blob/main/").
// If any git command fails, returns empty strings silently.
func detectRepoURL() (string, string) {
	repoRoot, err := gitOutput("rev-parse", "--show-toplevel")
	if err != nil {
		return "", ""
	}

	remote, err := gitOutput("remote", "get-url", "origin")
	if err != nil {
		return "", ""
	}

	branch, err := gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", ""
	}

	baseURL, hosting := parseRemoteURL(remote)
	if baseURL == "" {
		return "", repoRoot
	}

	var repoURL string
	switch hosting {
	case "gitlab":
		repoURL = fmt.Sprintf("%s/-/blob/%s/", baseURL, branch)
	case "bitbucket":
		repoURL = fmt.Sprintf("%s/src/%s/", baseURL, branch)
	default: // github and others
		repoURL = fmt.Sprintf("%s/blob/%s/", baseURL, branch)
	}

	return repoURL, repoRoot
}

// sshRemoteRegex matches SSH-style git remotes: git@host:user/repo.git
var sshRemoteRegex = regexp.MustCompile(`^git@([^:]+):(.+?)(?:\.git)?$`)

// parseRemoteURL converts a git remote URL (HTTPS or SSH) into a web base URL
// and a hosting type ("github", "gitlab", "bitbucket", or "other").
// Returns ("", "") if the format is not recognized.
func parseRemoteURL(remote string) (baseURL, hosting string) {
	remote = strings.TrimSpace(remote)

	// SSH format: git@github.com:user/repo.git
	if m := sshRemoteRegex.FindStringSubmatch(remote); m != nil {
		host := m[1]
		path := m[2]
		baseURL = "https://" + host + "/" + path
		return baseURL, detectHosting(host)
	}

	// HTTPS format: https://github.com/user/repo.git
	if strings.HasPrefix(remote, "https://") || strings.HasPrefix(remote, "http://") {
		baseURL = strings.TrimSuffix(remote, ".git")
		host := extractHost(baseURL)
		return baseURL, detectHosting(host)
	}

	return "", ""
}

// detectHosting returns the hosting provider based on the hostname.
func detectHosting(host string) string {
	switch {
	case strings.Contains(host, "github"):
		return "github"
	case strings.Contains(host, "gitlab"):
		return "gitlab"
	case strings.Contains(host, "bitbucket"):
		return "bitbucket"
	default:
		return "other"
	}
}

// extractHost extracts the hostname from an HTTP(S) URL.
func extractHost(url string) string {
	// Strip scheme
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	// Take up to the first /
	if host, _, found := strings.Cut(url, "/"); found {
		return host
	}
	return url
}

// findRepoRoot walks up from the current working directory looking for a .git
// directory. Returns the path containing .git, or empty string if not found.
// This works without the git binary installed.
func findRepoRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, ".git")); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// gitOutput runs a git command and returns its trimmed stdout.
func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
