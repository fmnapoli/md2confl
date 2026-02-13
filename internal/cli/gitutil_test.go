// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import "testing"

func TestParseRemoteURL(t *testing.T) {
	tests := []struct {
		name        string
		remote      string
		wantBaseURL string
		wantHosting string
	}{
		{
			name:        "GitHub HTTPS",
			remote:      "https://github.com/fmnapoli/md2confl.git",
			wantBaseURL: "https://github.com/fmnapoli/md2confl",
			wantHosting: "github",
		},
		{
			name:        "GitHub HTTPS without .git",
			remote:      "https://github.com/fmnapoli/md2confl",
			wantBaseURL: "https://github.com/fmnapoli/md2confl",
			wantHosting: "github",
		},
		{
			name:        "GitHub SSH",
			remote:      "git@github.com:fmnapoli/md2confl.git",
			wantBaseURL: "https://github.com/fmnapoli/md2confl",
			wantHosting: "github",
		},
		{
			name:        "GitHub SSH without .git",
			remote:      "git@github.com:fmnapoli/md2confl",
			wantBaseURL: "https://github.com/fmnapoli/md2confl",
			wantHosting: "github",
		},
		{
			name:        "GitLab HTTPS",
			remote:      "https://gitlab.com/team/project.git",
			wantBaseURL: "https://gitlab.com/team/project",
			wantHosting: "gitlab",
		},
		{
			name:        "GitLab SSH",
			remote:      "git@gitlab.com:team/project.git",
			wantBaseURL: "https://gitlab.com/team/project",
			wantHosting: "gitlab",
		},
		{
			name:        "Bitbucket HTTPS",
			remote:      "https://bitbucket.org/team/repo.git",
			wantBaseURL: "https://bitbucket.org/team/repo",
			wantHosting: "bitbucket",
		},
		{
			name:        "Bitbucket SSH",
			remote:      "git@bitbucket.org:team/repo.git",
			wantBaseURL: "https://bitbucket.org/team/repo",
			wantHosting: "bitbucket",
		},
		{
			name:        "Self-hosted unknown",
			remote:      "https://git.corp.com/team/repo.git",
			wantBaseURL: "https://git.corp.com/team/repo",
			wantHosting: "other",
		},
		{
			name:        "Empty remote",
			remote:      "",
			wantBaseURL: "",
			wantHosting: "",
		},
		{
			name:        "Unrecognized format",
			remote:      "svn://example.com/repo",
			wantBaseURL: "",
			wantHosting: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baseURL, hosting := parseRemoteURL(tt.remote)
			if baseURL != tt.wantBaseURL {
				t.Errorf("baseURL = %q, want %q", baseURL, tt.wantBaseURL)
			}
			if hosting != tt.wantHosting {
				t.Errorf("hosting = %q, want %q", hosting, tt.wantHosting)
			}
		})
	}
}

func TestDetectHosting(t *testing.T) {
	tests := []struct {
		host string
		want string
	}{
		{"github.com", "github"},
		{"gitlab.com", "gitlab"},
		{"bitbucket.org", "bitbucket"},
		{"git.corp.com", "other"},
	}

	for _, tt := range tests {
		t.Run(tt.host, func(t *testing.T) {
			if got := detectHosting(tt.host); got != tt.want {
				t.Errorf("detectHosting(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		url  string
		want string
	}{
		{"https://github.com/user/repo", "github.com"},
		{"http://gitlab.com/user/repo", "gitlab.com"},
		{"https://git.corp.com", "git.corp.com"},
	}

	for _, tt := range tests {
		t.Run(tt.url, func(t *testing.T) {
			if got := extractHost(tt.url); got != tt.want {
				t.Errorf("extractHost(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}
