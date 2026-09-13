package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digiogithub/git-in-track/internal/syncengine"
	"github.com/digiogithub/git-in-track/internal/vault"
	"github.com/digiogithub/git-in-track/internal/youtrack"
)

// The shared harness of the four YouTrack job kinds.
//
// Nothing here touches the network and nothing here sleeps: the YouTrack
// instance is a fake that answers from a table, and the clock is the fixed one
// Options.Now installs, so a stamp written into a file is the same on every run
// and on every machine.

// jobClock is the instant every job test runs at.
var jobClock = time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)

// fakeYouTrack is a YouTrack instance with no wire.
//
// Every field is what the next call answers with and every counter is what the
// assertions read; the mutex is there because the engine runs handlers on its
// own goroutines and the race detector is part of the contract.
type fakeYouTrack struct {
	mu sync.Mutex

	// queries records every search query, which is how the ordering clause is
	// asserted, and pages the $top/$skip window each one asked for.
	queries []string
	pages   []youtrack.Page
	// issueIDs is the set the search answers with, paged.
	issueIDs []string
	// issues answers Issue by readable id; a missing id yields a bare issue.
	issues map[string]youtrack.Issue

	// attachments answers Attachments by issue id, and blobs the bytes each
	// attachment id downloads as.
	attachments map[string][]youtrack.Attachment
	blobs       map[string][]byte
	// downloadErr fails the next download of the attachment it names.
	downloadErr map[string]error
	// onDownload runs before a download, which is where a test cancels a job
	// mid-transfer.
	onDownload func(youtrack.Attachment)
	downloads  []string
	// onIssue runs on every issue read, counting from one, which is where a
	// test cancels a job between batches.
	onIssue func(n int)
	reads   int

	// comments records the comments created and edited.
	created []string
	edited  []string
	nextID  int
	postErr error

	// articles answers Article by id, children ChildArticles, and the two
	// slices record what was created and updated.
	articles     map[string]youtrack.Article
	children     map[string][]youtrack.ArticleRef
	searchResult []youtrack.Article
	createdArts  []youtrack.ArticleInput
	updatedArts  []string
	nextArticle  int

	// err fails every call that has no more specific failure of its own.
	err error
}

// newFakeYouTrack builds an empty instance.
func newFakeYouTrack() *fakeYouTrack {
	return &fakeYouTrack{
		issues:      map[string]youtrack.Issue{},
		attachments: map[string][]youtrack.Attachment{},
		blobs:       map[string][]byte{},
		downloadErr: map[string]error{},
		articles:    map[string]youtrack.Article{},
		children:    map[string][]youtrack.ArticleRef{},
	}
}

// SearchIssues answers one page of the configured id set.
func (f *fakeYouTrack) SearchIssues(_ context.Context, query string, page youtrack.Page) ([]youtrack.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, query)
	f.pages = append(f.pages, page)
	if f.err != nil {
		return nil, f.err
	}
	top := page.Top
	if top <= 0 {
		top = 50
	}
	out := []youtrack.Issue{}
	for i := page.Skip; i < len(f.issueIDs) && len(out) < top; i++ {
		out = append(out, youtrack.Issue{IDReadable: f.issueIDs[i], Summary: f.issueIDs[i]})
	}
	return out, nil
}

// SearchAllIssues is unused by the handlers and answers the whole set.
func (f *fakeYouTrack) SearchAllIssues(ctx context.Context, query string, page youtrack.Page) ([]youtrack.Issue, error) {
	return f.SearchIssues(ctx, query, youtrack.Page{Top: 1000, Skip: page.Skip})
}

// Issue answers the configured issue, or a bare one carrying the id.
func (f *fakeYouTrack) Issue(_ context.Context, id string) (youtrack.Issue, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	if f.onIssue != nil {
		f.onIssue(f.reads)
	}
	if f.err != nil {
		return youtrack.Issue{}, f.err
	}
	if issue, ok := f.issues[id]; ok {
		return issue, nil
	}
	return youtrack.Issue{ID: "1-" + id, IDReadable: id, Summary: "Issue " + id}, nil
}

// IssueLinks answers no links.
func (f *fakeYouTrack) IssueLinks(context.Context, string) ([]youtrack.IssueLink, error) {
	return nil, nil
}

// AllComments answers no comments.
func (f *fakeYouTrack) AllComments(context.Context, string) ([]youtrack.Comment, error) {
	return nil, nil
}

// Attachments answers the configured list.
func (f *fakeYouTrack) Attachments(_ context.Context, id string) ([]youtrack.Attachment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return f.attachments[id], nil
}

// DownloadAttachment answers the configured bytes.
func (f *fakeYouTrack) DownloadAttachment(
	_ context.Context, attachment youtrack.Attachment,
) (io.ReadCloser, error) {
	f.mu.Lock()
	hook := f.onDownload
	f.downloads = append(f.downloads, attachment.ID)
	err := f.downloadErr[attachment.ID]
	body := f.blobs[attachment.ID]
	f.mu.Unlock()

	if hook != nil {
		hook(attachment)
	}
	if err != nil {
		return nil, err
	}
	return io.NopCloser(strings.NewReader(string(body))), nil
}

// AddComment records a created comment and answers with a fresh id.
func (f *fakeYouTrack) AddComment(_ context.Context, id, text string) (youtrack.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.postErr != nil {
		return youtrack.Comment{}, f.postErr
	}
	f.nextID++
	f.created = append(f.created, id+"|"+text)
	return youtrack.Comment{ID: fmt.Sprintf("4-%d", f.nextID), Text: text}, nil
}

// UpdateComment edits a comment that was already pushed, recording the edit so
// a test can prove a re-delivered job updated rather than duplicated.
func (f *fakeYouTrack) UpdateComment(_ context.Context, id, commentID, text string) (youtrack.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.postErr != nil {
		return youtrack.Comment{}, f.postErr
	}
	f.edited = append(f.edited, id+"|"+commentID+"|"+text)
	return youtrack.Comment{ID: commentID, Text: text}, nil
}

// Article answers the configured article.
func (f *fakeYouTrack) Article(_ context.Context, id string) (youtrack.Article, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	article, ok := f.articles[id]
	if !ok {
		return youtrack.Article{}, &youtrack.APIError{Status: 404, Method: "GET", Path: "/api/articles/" + id}
	}
	return article, nil
}

// CreateArticle records the creation and answers with a fresh article.
func (f *fakeYouTrack) CreateArticle(_ context.Context, in youtrack.ArticleInput) (youtrack.Article, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return youtrack.Article{}, f.err
	}
	f.nextArticle++
	f.createdArts = append(f.createdArts, in)
	article := youtrack.Article{
		ID:         fmt.Sprintf("7-%d", f.nextArticle),
		IDReadable: fmt.Sprintf("DEMO-A-%d", f.nextArticle),
	}
	if in.Summary != nil {
		article.Summary = *in.Summary
	}
	if in.Content != nil {
		article.Content = *in.Content
	}
	if in.ParentArticleID != nil {
		article.ParentArticle = youtrack.ArticleRef{IDReadable: *in.ParentArticleID}
		f.children[*in.ParentArticleID] = append(f.children[*in.ParentArticleID],
			youtrack.ArticleRef{ID: article.ID, IDReadable: article.IDReadable, Summary: article.Summary})
	}
	f.articles[article.IDReadable] = article
	return article, nil
}

// UpdateArticle records the update and answers with the merged article.
func (f *fakeYouTrack) UpdateArticle(
	_ context.Context, id string, in youtrack.ArticleInput,
) (youtrack.Article, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return youtrack.Article{}, f.err
	}
	f.updatedArts = append(f.updatedArts, id)
	article := f.articles[id]
	article.IDReadable = id
	if in.Summary != nil {
		article.Summary = *in.Summary
	}
	if in.Content != nil {
		article.Content = *in.Content
	}
	f.articles[id] = article
	return article, nil
}

// ChildArticles answers the configured children.
func (f *fakeYouTrack) ChildArticles(_ context.Context, id string) ([]youtrack.ArticleRef, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.children[id], nil
}

// SearchArticles answers the configured search result and records the query.
func (f *fakeYouTrack) SearchArticles(
	_ context.Context, query string, _ youtrack.Page,
) ([]youtrack.Article, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queries = append(f.queries, query)
	if f.searchResult != nil {
		return f.searchResult, nil
	}
	// A real instance answers a query; answering from what was created is what
	// makes a re-publish able to find the parent article it made last time.
	out := make([]youtrack.Article, 0, len(f.articles))
	for _, article := range f.articles {
		out = append(out, article)
	}
	return out, nil
}

// fakeEditableYouTrack is a fake that can edit a comment, which the shipped
// client cannot yet.
type fakeEditableYouTrack struct{ *fakeYouTrack }

// UpdateComment records an edit.
func (f *fakeEditableYouTrack) UpdateComment(
	_ context.Context, id, commentID, text string,
) (youtrack.Comment, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.edited = append(f.edited, id+"|"+commentID+"|"+text)
	return youtrack.Comment{ID: commentID, Text: text}, nil
}

// ------------------------------------------------------------- the server ---

// newJobServer mounts the fixture with the fake instance installed as the
// client every job resolves, and returns the server and the served directory.
func newJobServer(t *testing.T, client youtrackJobClient) (*Server, string) {
	t.Helper()

	return newJobServerIn(t, client, copyTree(t, fixtureRoot))
}

// newJobServerIn is newJobServer over a tree the caller has already prepared,
// which is what a test that has to seed an `external` reference needs: the
// vault indexes the files New is given, so a fixture edit has to happen first.
func newJobServerIn(t *testing.T, client youtrackJobClient, root string) (*Server, string) {
	t.Helper()

	s, err := New(Options{
		Token:     "test-token",
		Version:   "0.0.1-test",
		Workspace: "test",
		Repos:     []Repo{{ID: testRepoID, Path: root, Role: "project", DocsFolder: "docs"}},
		Now:       func() time.Time { return jobClock },
		// A negative debounce dispatches a job the moment it is enqueued, which
		// is what makes an engine test finish without waiting on a clock.
		SyncEngine: SyncEngine{Debounce: -1},
	})
	if err != nil {
		t.Fatalf("New(): %v", err)
	}
	s.youtrack.mu.Lock()
	s.youtrack.jobClient = func(string) (youtrackJobClient, vault.YouTrackLink, error) {
		return client, vault.YouTrackLink{BaseURL: "https://yt.example.com/youtrack", Project: "DEMO"}, nil
	}
	s.youtrack.mu.Unlock()
	return s, root
}

// runJob hands one payload straight to a handler, which is what a test wants:
// the engine's own scheduling is covered by internal/syncengine and repeating
// it here would only add a timer.
func runJob(
	ctx context.Context, t *testing.T, handler func(context.Context, []syncengine.Job) error,
	kind syncengine.Kind, envelope youtrackJobPayload, params any,
) error {
	t.Helper()

	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatalf("encode the parameters: %v", err)
	}
	envelope.Params = raw
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("encode the payload: %v", err)
	}
	return handler(ctx, []syncengine.Job{{ID: "job_1", Kind: kind, Key: "DEMO", Payload: body}})
}

// progressEvents collects the `sync.job.progress` payloads the hub buffered.
func progressEvents(t *testing.T, s *Server) []syncJobProgressData {
	t.Helper()

	events, _ := s.hub.since(0)
	out := make([]syncJobProgressData, 0, len(events))
	for _, ev := range events {
		if ev.Type != eventSyncJobProgress {
			continue
		}
		data, ok := ev.Data.(syncJobProgressData)
		if !ok {
			continue
		}
		out = append(out, data)
	}
	return out
}

// ------------------------------------------------------------- the import ---

// TestYouTrackImportPagesWithAnOrderedQuery is the paging contract of
// GIT-US-0050: every query carries an ordering clause and the walk moves with
// $top and $skip. Without the clause a $skip walk over a live instance silently
// skips and duplicates rows.
func TestYouTrackImportPagesWithAnOrderedQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name: "an empty query is scoped to the project and ordered",
			want: "project: {DEMO} order by: created asc",
		},
		{
			name:  "a query is scoped and ordered",
			query: "#Unresolved",
			want:  "project: {DEMO} #Unresolved order by: created asc",
		},
		{
			name:  "a query that orders itself is left alone",
			query: "project: {OTHER} order by: updated desc",
			want:  "project: {DEMO} project: {OTHER} order by: updated desc",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fake := newFakeYouTrack()
			fake.issueIDs = []string{"DEMO-1"}
			s, _ := newJobServer(t, fake)

			err := runJob(t.Context(), t, s.handleYouTrackImportJobs, kindYouTrackImport,
				youtrackJobPayload{Repo: testRepoID, Project: "DEMO"},
				YouTrackImportParams{Query: tc.query})
			if err != nil {
				t.Fatalf("the import failed: %v", err)
			}
			if len(fake.queries) == 0 {
				t.Fatal("the import made no search")
			}
			if fake.queries[0] != tc.want {
				t.Errorf("query = %q, want %q", fake.queries[0], tc.want)
			}
		})
	}
}

// TestYouTrackImportWalksEveryPageOnce proves the $skip walk advances and stops
// on a short page.
func TestYouTrackImportWalksEveryPageOnce(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	for i := 1; i <= importSearchPageSize+5; i++ {
		fake.issueIDs = append(fake.issueIDs, fmt.Sprintf("DEMO-%d", i))
	}
	s, _ := newJobServer(t, fake)

	err := runJob(t.Context(), t, s.handleYouTrackImportJobs, kindYouTrackImport,
		youtrackJobPayload{Repo: testRepoID, Project: "DEMO"},
		YouTrackImportParams{BatchSize: 50})
	if err != nil {
		t.Fatalf("the import failed: %v", err)
	}
	if len(fake.pages) != 2 {
		t.Fatalf("the walk made %d requests, want 2", len(fake.pages))
	}
	if fake.pages[0].Skip != 0 || fake.pages[0].Top != importSearchPageSize {
		t.Errorf("first page = %+v, want skip 0", fake.pages[0])
	}
	if fake.pages[1].Skip != importSearchPageSize {
		t.Errorf("second page skip = %d, want %d", fake.pages[1].Skip, importSearchPageSize)
	}
}

// TestYouTrackImportRunsInBatches covers the batching and the progress contract
// together: one progress event per batch, carrying {jobId, done, total,
// currentId}.
func TestYouTrackImportRunsInBatches(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	fake.issueIDs = []string{"DEMO-1", "DEMO-2", "DEMO-3", "DEMO-4", "DEMO-5"}
	s, _ := newJobServer(t, fake)

	err := runJob(t.Context(), t, s.handleYouTrackImportJobs, kindYouTrackImport,
		youtrackJobPayload{Repo: testRepoID, Project: "DEMO"},
		YouTrackImportParams{BatchSize: 2})
	if err != nil {
		t.Fatalf("the import failed: %v", err)
	}

	events := progressEvents(t, s)
	if len(events) != 4 {
		t.Fatalf("the import published %d progress events, want 4 (one opening plus one per batch)", len(events))
	}
	wantDone := []int{0, 2, 4, 5}
	for i, ev := range events {
		if ev.JobID != "job_1" || ev.ID != "job_1" {
			t.Errorf("event %d job id = %q/%q, want job_1", i, ev.JobID, ev.ID)
		}
		if ev.Total != 5 {
			t.Errorf("event %d total = %d, want 5", i, ev.Total)
		}
		if ev.Done != wantDone[i] || ev.Processed != wantDone[i] {
			t.Errorf("event %d done = %d/%d, want %d", i, ev.Done, ev.Processed, wantDone[i])
		}
	}
	if events[len(events)-1].CurrentID != "DEMO-5" {
		t.Errorf("the last event names %q, want DEMO-5", events[len(events)-1].CurrentID)
	}
}

// TestYouTrackImportAcceptsAnExplicitIDList proves an id list skips the search
// entirely, which is the path the import dialog uses once a user has picked
// rows.
func TestYouTrackImportAcceptsAnExplicitIDList(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	s, _ := newJobServer(t, fake)

	err := runJob(t.Context(), t, s.handleYouTrackImportJobs, kindYouTrackImport,
		youtrackJobPayload{Repo: testRepoID, Project: "DEMO"},
		YouTrackImportParams{IDs: []string{"DEMO-7", "  ", "DEMO-8"}})
	if err != nil {
		t.Fatalf("the import failed: %v", err)
	}
	if len(fake.queries) != 0 {
		t.Errorf("an id list searched anyway: %v", fake.queries)
	}
	events := progressEvents(t, s)
	if len(events) == 0 || events[0].Total != 2 {
		t.Fatalf("the blank id was not dropped: %+v", events)
	}
}

// TestYouTrackImportFailsTerminallyOnAnUnroutableJob pins the classification: a
// job naming a repository this companion does not serve can never succeed, so
// it must not spend the retry budget.
func TestYouTrackImportFailsTerminallyOnAnUnroutableJob(t *testing.T) {
	t.Parallel()

	s, _ := newJobServer(t, newFakeYouTrack())
	err := runJob(t.Context(), t, s.handleYouTrackImportJobs, kindYouTrackImport,
		youtrackJobPayload{Repo: "nowhere", Project: "NOPE"}, YouTrackImportParams{})
	if err == nil {
		t.Fatal("an unroutable job succeeded")
	}
	if class := syncengine.Classify(err, jobClock).Class; class != syncengine.ClassTerminal {
		t.Errorf("class = %s, want terminal", class)
	}
}

// TestYouTrackAPIErrorsAreClassified covers the reason
// classifyYouTrackJobError exists: youtrack.APIError carries its status in a
// field, so the engine cannot read it without help.
func TestYouTrackAPIErrorsAreClassified(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status int
		want   syncengine.ErrorClass
	}{
		{name: "a rejected token is terminal", status: 401, want: syncengine.ClassTerminal},
		{name: "a missing permission is terminal", status: 403, want: syncengine.ClassTerminal},
		{name: "a missing article is terminal", status: 404, want: syncengine.ClassTerminal},
		{name: "throttling is retryable", status: 429, want: syncengine.ClassRetryable},
		{name: "a server failure is retryable", status: 503, want: syncengine.ClassRetryable},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := classifyYouTrackJobError(fmt.Errorf("call failed: %w",
				&youtrack.APIError{Status: tc.status, Method: "GET", Path: "/api/issues/DEMO-1"}))
			if class := syncengine.Classify(err, jobClock).Class; class != tc.want {
				t.Errorf("class = %s, want %s", class, tc.want)
			}
		})
	}
}

// TestYouTrackImportCancelsBetweenBatches proves the first half of the
// cancellation contract: the run stops between batches, the batches that
// already committed survive, and the failure the engine sees is a cancellation
// rather than an error worth retrying.
func TestYouTrackImportCancelsBetweenBatches(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	fake.issueIDs = []string{"DEMO-1", "DEMO-2", "DEMO-3", "DEMO-4"}
	s, root := newJobServer(t, fake)

	ctx, cancel := context.WithCancel(t.Context())
	// Cancelled while the second batch is being read, which is after the first
	// one has been written and committed. The cancellation is therefore
	// observed by the handler with one batch already on disk, which is exactly
	// the state the contract is about.
	fake.onIssue = func(n int) {
		if n == 2 {
			cancel()
		}
	}

	err := runJob(ctx, t, s.handleYouTrackImportJobs, kindYouTrackImport,
		youtrackJobPayload{Repo: testRepoID, Project: "DEMO"},
		YouTrackImportParams{BatchSize: 1})
	if err == nil {
		t.Fatal("the cancelled import reported success")
	}
	if class := syncengine.Classify(err, jobClock).Class; class != syncengine.ClassCancelled {
		t.Fatalf("class = %s, want cancelled", class)
	}
	// The first batch is still on disk: a cancellation keeps what was written.
	matches, _ := filepath.Glob(filepath.Join(root, "docs", ".pmngr", "tasks", "*.md"))
	if len(matches) == 0 {
		t.Error("the committed batch did not survive the cancellation")
	}
}

// ---------------------------------------------------------- attachments ---

// TestDownloadAttachmentsWritesRenamesAndSkips covers GIT-T-0072 end to end: a
// download streams to a `.part` file and is renamed on success, a file that is
// already there at the right size is skipped, and a failed download leaves no
// temporary file behind.
func TestDownloadAttachmentsWritesRenamesAndSkips(t *testing.T) {
	t.Parallel()

	body := []byte("a diagram")
	fake := newFakeYouTrack()
	fake.attachments["DEMO-1"] = []youtrack.Attachment{
		{ID: "8-1", Name: "diagram.png", Size: int64(len(body)), URL: "/api/files/8-1?sign=x"},
	}
	fake.blobs["8-1"] = body
	s, _ := newJobServer(t, fake)
	root := t.TempDir()

	t.Run("the first download lands and no part file survives", func(t *testing.T) {
		if err := s.downloadAttachments(t.Context(), fake, root, "DEMO-1", "DEMO-T-0001"); err != nil {
			t.Fatalf("download: %v", err)
		}
		target := filepath.Join(root, "DEMO-T-0001", "diagram.png")
		got, err := os.ReadFile(target)
		if err != nil {
			t.Fatalf("the attachment was not installed: %v", err)
		}
		if string(got) != string(body) {
			t.Errorf("content = %q, want %q", got, body)
		}
		if parts, _ := filepath.Glob(filepath.Join(root, "DEMO-T-0001", "*.part")); len(parts) != 0 {
			t.Errorf("a partial file survived: %v", parts)
		}
	})

	t.Run("a second run skips a file that is already the right size", func(t *testing.T) {
		before := len(fake.downloads)
		if err := s.downloadAttachments(t.Context(), fake, root, "DEMO-1", "DEMO-T-0001"); err != nil {
			t.Fatalf("download: %v", err)
		}
		if len(fake.downloads) != before {
			t.Errorf("the file was downloaded again: %v", fake.downloads)
		}
	})

	t.Run("a failed download leaves nothing behind", func(t *testing.T) {
		fake.attachments["DEMO-2"] = []youtrack.Attachment{
			{ID: "8-2", Name: "broken.bin", Size: 12, URL: "/api/files/8-2?sign=y"},
		}
		fake.downloadErr["8-2"] = &youtrack.APIError{Status: 500, Method: "GET", Path: "/api/files/8-2"}
		err := s.downloadAttachments(t.Context(), fake, root, "DEMO-2", "DEMO-T-0002")
		if err == nil {
			t.Fatal("a failed download reported success")
		}
		entries, _ := os.ReadDir(filepath.Join(root, "DEMO-T-0002"))
		if len(entries) != 0 {
			t.Errorf("the failed download left %d files behind", len(entries))
		}
	})

	t.Run("a short body is refused rather than installed", func(t *testing.T) {
		fake.attachments["DEMO-3"] = []youtrack.Attachment{
			{ID: "8-3", Name: "truncated.bin", Size: 999, URL: "/api/files/8-3"},
		}
		fake.blobs["8-3"] = []byte("short")
		err := s.downloadAttachments(t.Context(), fake, root, "DEMO-3", "DEMO-T-0003")
		if err == nil {
			t.Fatal("a truncated download reported success")
		}
		if _, statErr := os.Stat(filepath.Join(root, "DEMO-T-0003", "truncated.bin")); statErr == nil {
			t.Error("a truncated download was installed")
		}
	})
}

// TestDownloadAttachmentsStopMidTransfer is the second observation point of the
// cancellation contract: a download in flight is abandoned and its temporary
// file is removed.
func TestDownloadAttachmentsStopMidTransfer(t *testing.T) {
	t.Parallel()

	fake := newFakeYouTrack()
	fake.attachments["DEMO-1"] = []youtrack.Attachment{
		{ID: "8-1", Name: "first.bin", Size: 4, URL: "/api/files/8-1"},
		{ID: "8-2", Name: "second.bin", Size: 4, URL: "/api/files/8-2"},
	}
	fake.blobs["8-1"] = []byte("abcd")
	fake.blobs["8-2"] = []byte("efgh")
	s, _ := newJobServer(t, fake)
	root := t.TempDir()

	ctx, cancel := context.WithCancel(t.Context())
	fake.onDownload = func(attachment youtrack.Attachment) {
		if attachment.ID == "8-1" {
			cancel()
		}
	}

	err := s.downloadAttachments(ctx, fake, root, "DEMO-1", "DEMO-T-0001")
	if err == nil {
		t.Fatal("the cancelled download reported success")
	}
	if class := syncengine.Classify(err, jobClock).Class; class != syncengine.ClassCancelled {
		t.Errorf("class = %s, want cancelled", class)
	}
	if parts, _ := filepath.Glob(filepath.Join(root, "DEMO-T-0001", "*.part")); len(parts) != 0 {
		t.Errorf("a partial file survived the cancellation: %v", parts)
	}
	if _, statErr := os.Stat(filepath.Join(root, "DEMO-T-0001", "second.bin")); statErr == nil {
		t.Error("the download continued after the cancellation")
	}
}

// TestAttachmentFileNameStaysInsideTheFolder pins the one thing a remote name
// must never be able to do.
func TestAttachmentFileNameStaysInsideTheFolder(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"diagram.png":            "diagram.png",
		"  spaced.png  ":         "spaced.png",
		"../../etc/passwd":       "passwd",
		`..\..\windows\file.bin`: "file.bin",
		"..":                     "",
		"":                       "",
	}
	for raw, want := range tests {
		if got := attachmentFileName(raw); got != want {
			t.Errorf("attachmentFileName(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestPartFileNameIsUnique pins the shape of the temporary name: the target,
// the process id and a timestamp.
func TestPartFileNameIsUnique(t *testing.T) {
	t.Parallel()

	name := partFileName("/tmp/x/diagram.png", jobClock)
	if !strings.HasPrefix(name, "/tmp/x/diagram.png.") || !strings.HasSuffix(name, ".part") {
		t.Fatalf("part name = %q", name)
	}
	if !strings.Contains(name, fmt.Sprintf(".%d.", os.Getpid())) {
		t.Errorf("part name %q does not carry the process id", name)
	}
	if name == partFileName("/tmp/x/diagram.png", jobClock.Add(time.Second)) {
		t.Error("two instants produced the same part name")
	}
}
