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

	// Texto sem acento: o corpo volta do servidor com os não-ASCII em entidades.
	edited := docs["docs/tdn/guides/configuration.md"] + "\nA brand new paragraph.\n"
	if err := os.WriteFile(filepath.Join(dir, "docs/tdn/guides/configuration.md"), []byte(edited), 0644); err != nil {
		t.Fatal(err)
	}

	runPublish(t, cfgPath)
	after := fake.pageVersions()

	if after["Configuration"] <= before["Configuration"] {
		t.Errorf("the edited page was not republished (version %d → %d)",
			before["Configuration"], after["Configuration"])
	}
	if conf := fake.pageByTitle("Configuration"); conf == nil || !strings.Contains(conf.Body, "A brand new paragraph.") {
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

// TestServerPublish_DiscardedDigestIsReportedAndSafe is the test that the HTML
// comment marker would have failed. The comment was accepted by Confluence and
// silently dropped from the stored body, so every run read a page with no
// marker, concluded "changed" and republished — and the fake, which kept
// whatever it was given, showed none of it.
//
// Here the server accepts the content property write and keeps nothing. Two
// things must hold: the run has to say so out loud, and it must still be
// correct — never skipping a page it cannot prove is unchanged.
func TestServerPublish_DiscardedDigestIsReportedAndSafe(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)
	fake.dropProperties = true
	cfgPath := setupConsumerRepo(t, dir, ts.URL, consumerDocs())

	stderr := runPublish(t, cfgPath)
	if !strings.Contains(stderr, "did not keep the md2confl source digest") {
		t.Errorf("the run must report that the digest was not persisted; stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, digestPropertyKey) {
		t.Errorf("the warning must name the content property; stderr:\n%s", stderr)
	}

	before := fake.pageVersions()
	runPublish(t, cfgPath)
	after := fake.pageVersions()

	// Sem digest não há como provar que nada mudou: republicar é a resposta
	// certa. O que não pode acontecer é pular por engano.
	republished := 0
	for _, title := range sortedTitles(before) {
		if after[title] > before[title] {
			republished++
		}
	}
	if republished == 0 {
		t.Error("without a persisted digest the pages must be republished, not skipped")
	}
}

// TestServerPublish_SanitizedBodyStillSkips guards the reason the digest lives
// outside the body: Confluence rewrites the Storage Format it stores (comments
// dropped, ac:macro-id injected, accents escaped), so the published body of an
// untouched page never equals the HTML that generated it.
func TestServerPublish_SanitizedBodyStillSkips(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)
	docs := consumerDocs()
	// Documentação em pt-br: acentos são o caso comum, não a exceção.
	docs["docs/tdn/guides/configuration.md"] = "# Configuration\n\nConfiguração de operação.\n"
	cfgPath := setupConsumerRepo(t, dir, ts.URL, docs)

	runPublish(t, cfgPath)

	conf := fake.pageByTitle("Configuration")
	if conf == nil {
		t.Fatal("configuration page was not published")
	}
	// Pré-condição do teste: o corpo publicado passou pelas reescritas do
	// servidor, senão a comparação byte a byte bastaria e o teste não provaria nada.
	if !strings.Contains(conf.Body, "ac:macro-id=") || !strings.Contains(conf.Body, "&#") {
		t.Fatalf("the fake did not rewrite the stored body:\n%s", conf.Body)
	}

	stderr := runPublish(t, cfgPath)
	if !strings.Contains(stderr, "Skipped (unchanged)") {
		t.Errorf("the second run must skip the page; stderr:\n%s", stderr)
	}
}

// publishSingleDoc writes a one-document config and returns its path. Keeping
// the tree to a single file isolates the digest bookkeeping from the second
// pass, which would otherwise rewrite the page for its own reasons.
func publishSingleDoc(t *testing.T, dir, baseURL, content string) string {
	t.Helper()
	cfg := `url: ` + baseURL + `
space: TEST
email: user@example.com
server: true
force: true
write-marker: false
documents:
  - input: doc.md
`
	cfgPath := writeConfigAndDocs(t, dir, cfg, map[string]string{"doc.md": content})
	t.Chdir(dir)
	t.Setenv("CONFLUENCE_TOKEN", "fake-token")
	return cfgPath
}

// TestServerPublish_LostDigestWriteDoesNotStrandContent is the false positive
// the review caught: the body write succeeds and the digest write does not, so
// the page keeps the digest of the PREVIOUS source while already showing the
// new body. Any later run whose source hashes to that stale digest — a revert,
// a rollback, re-running an older tag — is skipped, and the wrong content stays
// published with nothing in the log to show for it.
//
// The digest must therefore be invalidated BEFORE the body is written: a crash
// anywhere in between leaves no digest at all, which republishes.
func TestServerPublish_LostDigestWriteDoesNotStrandContent(t *testing.T) {
	tests := []struct {
		name   string
		break_ func(f *fakeConfluenceServer)
	}{
		// A instância aceita a escrita e não guarda nada.
		{"property descartada", func(f *fakeConfluenceServer) { f.dropProperties = true }},
		// A escrita falha (5xx, endpoint bloqueado, processo morto no meio).
		{"escrita de property falhando", func(f *fakeConfluenceServer) { f.failPropertySets = true }},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			ts, fake := newFakeConfluenceServer(t)
			const v1 = "# Doc\n\nVERSION ONE\n"
			const v2 = "# Doc\n\nVERSION TWO\n"
			cfgPath := publishSingleDoc(t, dir, ts.URL, v1)

			// V1 publicada normalmente, com digest gravado.
			runPublish(t, cfgPath)

			// V2 publicada sem que o digest acompanhe.
			tt.break_(fake)
			if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte(v2), 0644); err != nil {
				t.Fatal(err)
			}
			runPublish(t, cfgPath)
			if page := fake.pageByTitle("Doc"); !strings.Contains(page.Body, "VERSION TWO") {
				t.Fatalf("V2 was not published; body: %s", page.Body)
			}

			// Volta a fonte para V1 — como um revert no repositório.
			fake.dropProperties = false
			fake.failPropertySets = false
			if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte(v1), 0644); err != nil {
				t.Fatal(err)
			}
			stderr := runPublish(t, cfgPath)

			page := fake.pageByTitle("Doc")
			if strings.Contains(page.Body, "VERSION TWO") {
				t.Errorf("reverting the source left the old body published (skipped on a stale digest)\nstderr: %s\nbody: %s",
					stderr, page.Body)
			}
			if !strings.Contains(page.Body, "VERSION ONE") {
				t.Errorf("the reverted source was not published\nbody: %s", page.Body)
			}
		})
	}
}

// TestServerPublish_FailedInvalidationKeepsBody covers the deliberate choice
// made when the previous digest cannot be invalidated: the page is left alone
// and the document is reported as failed. Writing the body anyway would leave
// the new content under the old digest — the exact state that makes a later
// run skip a page it should have republished.
func TestServerPublish_FailedInvalidationKeepsBody(t *testing.T) {
	dir := t.TempDir()
	ts, fake := newFakeConfluenceServer(t)
	const v1 = "# Doc\n\nVERSION ONE\n"
	cfgPath := publishSingleDoc(t, dir, ts.URL, v1)

	runPublish(t, cfgPath)

	fake.failPropertyDeletes = true
	if err := os.WriteFile(filepath.Join(dir, "doc.md"), []byte("# Doc\n\nVERSION TWO\n"), 0644); err != nil {
		t.Fatal(err)
	}

	var stdout, stderr strings.Builder
	if code := Run([]string{"--config", cfgPath}, "test", &stdout, &stderr); code == 0 {
		t.Errorf("a run that cannot invalidate the digest must not report success")
	}
	if !strings.Contains(stderr.String(), "invalidating the source digest") {
		t.Errorf("the failure must name what went wrong; stderr:\n%s", stderr.String())
	}

	page := fake.pageByTitle("Doc")
	if strings.Contains(page.Body, "VERSION TWO") {
		t.Errorf("the body was advanced while the old digest was still in place\nbody: %s", page.Body)
	}

	// Com o servidor de volta ao normal, a publicação acontece.
	fake.failPropertyDeletes = false
	runPublish(t, cfgPath)
	if page := fake.pageByTitle("Doc"); !strings.Contains(page.Body, "VERSION TWO") {
		t.Errorf("the page was not published once the server recovered\nbody: %s", page.Body)
	}
}
