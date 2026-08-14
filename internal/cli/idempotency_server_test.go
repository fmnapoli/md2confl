// Copyright 2026 md2confl contributors
// SPDX-License-Identifier: Apache-2.0

package cli

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// pageVersions returns title → version for every page stored in the fake,
// so a test can assert that a second run produced no new page version.
func (f *fakeConfluenceServer) pageVersions() map[string]int {
	f.mu.Lock()
	defer f.mu.Unlock()
	versions := make(map[string]int, len(f.pages))
	for _, p := range f.pages {
		versions[p.Title] = p.Version
	}
	return versions
}

// runPublish runs the CLI against the fake server and fails the test on a
// non-zero exit code. It returns stderr, where the per-page log lines live.
func runPublish(t *testing.T, cfgPath string) string {
	t.Helper()
	var stdout, stderr strings.Builder
	if code := Run([]string{"--config", cfgPath}, "test", &stdout, &stderr); code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}
	return stderr.String()
}

// setBody overwrites the stored body of a page, emulating an edit made outside
// md2confl (an interrupted run, or a manual change in Confluence).
func (f *fakeConfluenceServer) setBody(id, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pages[id].Body = body
}

func sortedTitles(versions map[string]int) []string {
	titles := make([]string, 0, len(versions))
	for title := range versions {
		titles = append(titles, title)
	}
	sort.Strings(titles)
	return titles
}

// TestServerPublish_SecondRunIsIdempotent is the regression test for the
// defect: the idempotency check compared the freshly generated HTML (raw
// relative links) with the published body (links already rewritten by the
// second pass), so the two never matched and every run republished every page.
func TestServerPublish_SecondRunIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)

	cfgPath := writeConfigAndDocs(t, dir, serverConfig(ts.URL), consumerDocs())
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("CONFLUENCE_TOKEN", "fake-token")

	runPublish(t, cfgPath)
	first := fake.pageVersions()
	if len(first) == 0 {
		t.Fatal("no page was published in the first run")
	}

	runPublish(t, cfgPath)
	second := fake.pageVersions()

	for _, title := range sortedTitles(first) {
		if second[title] != first[title] {
			t.Errorf("page %q was republished: version %d → %d", title, first[title], second[title])
		}
	}
}

// setupConsumerRepo writes the consumer layout plus the .git directory that
// anchors the repo-url fallback, and returns the config path.
func setupConsumerRepo(t *testing.T, dir, baseURL string, docs map[string]string) string {
	t.Helper()
	cfgPath := writeConfigAndDocs(t, dir, serverConfig(baseURL), docs)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	t.Setenv("CONFLUENCE_TOKEN", "fake-token")
	return cfgPath
}

// TestServerPublish_ChangedDocIsRepublished is the counterpart of the
// idempotency test: skipping a page whose Markdown changed is the failure mode
// that must never happen, so only the edited page may be rewritten.
func TestServerPublish_ChangedDocIsRepublished(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)
	docs := consumerDocs()
	cfgPath := setupConsumerRepo(t, dir, ts.URL, docs)

	runPublish(t, cfgPath)
	before := fake.pageVersions()

	edited := docs["docs/tdn/guides/configuration.md"] + "\nUm parágrafo novo.\n"
	if err := os.WriteFile(filepath.Join(dir, "docs/tdn/guides/configuration.md"), []byte(edited), 0644); err != nil {
		t.Fatal(err)
	}

	runPublish(t, cfgPath)
	after := fake.pageVersions()

	if after["Configuration"] <= before["Configuration"] {
		t.Errorf("the edited page was not republished (version %d → %d)",
			before["Configuration"], after["Configuration"])
	}
	if conf := fake.pageByTitle("Configuration"); conf == nil || !strings.Contains(conf.Body, "Um parágrafo novo.") {
		t.Errorf("the new content was not published; body: %v", conf)
	}
	for _, title := range sortedTitles(before) {
		if title == "Configuration" {
			continue
		}
		if after[title] != before[title] {
			t.Errorf("untouched page %q was republished: version %d → %d", title, before[title], after[title])
		}
	}
}

// TestServerPublish_ChangedLinkTargetIsRepublished covers the case the digest
// exists for: the text around the link is identical and only the link target
// changed. Comparing the published body — where links are already rewritten to
// Confluence URLs — could not tell the two apart.
func TestServerPublish_ChangedLinkTargetIsRepublished(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)
	docs := consumerDocs()
	cfgPath := setupConsumerRepo(t, dir, ts.URL, docs)

	runPublish(t, cfgPath)

	// architecture.md aponta para o README da raiz; passa a apontar para o guia.
	retargeted := "# Arquitetura do TCloud Worker\n\nBack to [root](../guides/configuration.md).\n"
	if err := os.WriteFile(filepath.Join(dir, "docs/tdn/architecture/architecture.md"), []byte(retargeted), 0644); err != nil {
		t.Fatal(err)
	}

	runPublish(t, cfgPath)

	arch := fake.pageByTitle("Arquitetura do TCloud Worker")
	conf := fake.pageByTitle("Configuration")
	if arch == nil || conf == nil {
		t.Fatal("pages disappeared")
	}
	confURL := ts.URL + "/display/TEST/" + conf.ID
	if !strings.Contains(arch.Body, `href="`+confURL+`"`) {
		t.Errorf("the new link target was not published, want %s\nbody: %s", confURL, arch.Body)
	}
}

// TestServerPublish_RenamedPageIsPublished guards the end-to-end path of a
// rename, which the digest also covers through the title: the run must not
// treat the page as already published just because its body barely changed.
func TestServerPublish_RenamedPageIsPublished(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)
	docs := consumerDocs()
	docs["docs/tdn/guides/configuration.md"] = "# Configuration\n\nContent.\n"
	cfgPath := setupConsumerRepo(t, dir, ts.URL, docs)

	runPublish(t, cfgPath)
	before := fake.pageVersions()

	if err := os.WriteFile(filepath.Join(dir, "docs/tdn/guides/configuration.md"),
		[]byte("# Configuration Guide\n\nContent.\n"), 0644); err != nil {
		t.Fatal(err)
	}

	runPublish(t, cfgPath)

	if fake.pageByTitle("Configuration Guide") == nil {
		t.Errorf("the renamed page was not published; versions before: %v, after: %v", before, fake.pageVersions())
	}
}

// TestServerPublish_RawLinksAreRepairedAfterInterruption emulates a run killed
// between the two phases: the page went back to raw relative links. The source
// did not change, so the first phase skips the page — the second phase is what
// has to notice that the published body no longer matches and repair it.
func TestServerPublish_RawLinksAreRepairedAfterInterruption(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)
	cfgPath := setupConsumerRepo(t, dir, ts.URL, consumerDocs())

	runPublish(t, cfgPath)

	readme := fake.pageByTitle("TCloud Worker")
	arch := fake.pageByTitle("Arquitetura do TCloud Worker")
	if readme == nil || arch == nil {
		t.Fatal("pages were not published")
	}
	archURL := ts.URL + "/display/TEST/" + arch.ID
	fake.setBody(readme.ID, strings.Replace(readme.Body, archURL, "docs/tdn/architecture/architecture.md", 1))

	runPublish(t, cfgPath)

	repaired := fake.pageByTitle("TCloud Worker")
	if !strings.Contains(repaired.Body, `href="`+archURL+`"`) {
		t.Errorf("raw links were not repaired, want %s\nbody: %s", archURL, repaired.Body)
	}
}

// TestServerPublish_SingleDocumentKeepsResolvedLinks reproduces the incident
// report: publishing one document of the config on its own runs no link
// resolution pass, so republishing it would push the raw relative links over
// the resolved ones already in Confluence.
func TestServerPublish_SingleDocumentKeepsResolvedLinks(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)
	cfgPath := setupConsumerRepo(t, dir, ts.URL, consumerDocs())

	runPublish(t, cfgPath)
	arch := fake.pageByTitle("Arquitetura do TCloud Worker")
	if arch == nil {
		t.Fatal("architecture page was not published")
	}
	archURL := ts.URL + "/display/TEST/" + arch.ID
	before := fake.pageVersions()

	var stdout, stderr strings.Builder
	if code := Run([]string{"--config", cfgPath, "--input", "README.md"}, "test", &stdout, &stderr); code != 0 {
		t.Fatalf("expected exit code 0, got %d; stderr: %s", code, stderr.String())
	}

	readme := fake.pageByTitle("TCloud Worker")
	if !strings.Contains(readme.Body, `href="`+archURL+`"`) {
		t.Errorf("resolved links were overwritten with raw ones\nbody: %s", readme.Body)
	}
	if got := fake.pageVersions()["TCloud Worker"]; got != before["TCloud Worker"] {
		t.Errorf("page was republished: version %d → %d", before["TCloud Worker"], got)
	}
}
