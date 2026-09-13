package mcp

import (
	"context"
	"strings"
)

// The YouTrack tools: the agent half of the integration.
//
// All four are write tools, so a read-only server does not advertise them at
// all. They hold no business logic whatsoever — the import, the comment push
// and the two knowledge-base operations each have exactly one implementation,
// in internal/vault, and this file only validates arguments, guards the paths,
// dispatches and projects the answer. A rule that existed here and nowhere else
// would be a rule the web app and the CLI do not obey.
//
// The two knowledge-base tools and the comment push queue a background job
// rather than performing the work: a tool call returns a job id and the engine
// does the talking, so an agent is never blocked on a remote tracker and a
// dropped session cannot cancel a push in flight.

// ImportYouTrackIssuesInput selects the issues to import and how deep to go.
type ImportYouTrackIssuesInput struct {
	Project            string   `json:"project,omitempty" jsonschema:"Project key to import into; omit when the workspace holds one project"`
	Query              string   `json:"query,omitempty" jsonschema:"YouTrack issue query, for example 'State: Open #Unresolved'. Give this or ids, not both"`
	IDs                []string `json:"ids,omitempty" jsonschema:"Readable issue ids such as ACME-42. Give this or query, not both"`
	Depth              int      `json:"depth,omitempty" jsonschema:"Subtask recursion, 0 to 5; 0 imports the selected issues only"`
	IncludeLinks       bool     `json:"includeLinks,omitempty" jsonschema:"Import the non-hierarchy relations as links"`
	IncludeComments    bool     `json:"includeComments,omitempty" jsonschema:"Write the issue's comment thread as comment files"`
	IncludeAttachments bool     `json:"includeAttachments,omitempty" jsonschema:"Record the attachment paths on the item"`
	DryRun             bool     `json:"dryRun,omitempty" jsonschema:"Report what the import would do and write nothing"`
}

// ImportedIssue is one issue of the answer: what it became, or would become.
type ImportedIssue struct {
	YouTrackID string `json:"youtrackId"`
	Title      string `json:"title,omitempty"`
	// Action is "create" or "update": an issue an item already carries is
	// updated in place and keeps the id it has.
	Action string `json:"action,omitempty"`
	// ItemID is the git-in-track item, and Rev the token a later write must
	// quote. A dry run reports the item an update would patch and nothing for a
	// create, which has no id yet.
	ItemID   string   `json:"itemId,omitempty"`
	Rev      string   `json:"rev,omitempty"`
	Comments int      `json:"comments,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
	Error    string   `json:"error,omitempty"`
}

// ImportResult is the answer of import_youtrack_issues.
type ImportResult struct {
	Project string          `json:"project,omitempty"`
	DryRun  bool            `json:"dryRun"`
	Issues  []ImportedIssue `json:"issues"`
	Created int             `json:"created"`
	Updated int             `json:"updated"`
	Failed  int             `json:"failed"`
	// Changed lists the files the import wrote, empty on a dry run.
	Changed  []string `json:"changed,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// PushCommentInput selects the comments of one item to send upstream.
type PushCommentInput struct {
	Project     string `json:"project,omitempty" jsonschema:"Project key; omit to resolve it from the item id"`
	ItemID      string `json:"itemId" jsonschema:"Item whose comments are pushed, for example ACME-US-0042"`
	CommentPath string `json:"commentPath,omitempty" jsonschema:"Vault-relative path of one comment file. Give this or all, not both"`
	All         bool   `json:"all,omitempty" jsonschema:"Push every comment of the item that is not upstream yet"`
}

// PushedComment is one comment of the answer.
type PushedComment struct {
	CommentPath       string `json:"commentPath"`
	YouTrackCommentID string `json:"youtrackCommentId,omitempty"`
	URL               string `json:"url,omitempty"`
	Reason            string `json:"reason,omitempty"`
	Error             string `json:"error,omitempty"`
}

// PushCommentResult is the answer of push_comment_to_youtrack.
//
// Pushed is what was queued, not what has already arrived: the work happens in
// the job named by JobID.
type PushCommentResult struct {
	ItemID  string          `json:"itemId"`
	JobID   string          `json:"jobId,omitempty"`
	Pushed  []PushedComment `json:"pushed"`
	Skipped []PushedComment `json:"skipped"`
	Failed  []PushedComment `json:"failed"`
}

// KBSyncInput selects the pages one knowledge-base job covers.
type KBSyncInput struct {
	Project   string `json:"project,omitempty" jsonschema:"Project key owning the pages"`
	Path      string `json:"path" jsonschema:"Vault-relative path of a page or a folder, for example docs/architecture/auth.md"`
	Recursive bool   `json:"recursive,omitempty" jsonschema:"Include the pages of every folder below path"`
}

// KBSyncPage is one page the queued job covers.
type KBSyncPage struct {
	Path string `json:"path"`
	// Action is "publish" or "pull": what the queued job will do to this page.
	Action string `json:"action"`
	// ArticleID, URL and Error are filled by the job's own report, not by the
	// call that queued it.
	ArticleID string `json:"articleId,omitempty"`
	URL       string `json:"url,omitempty"`
	Error     string `json:"error,omitempty"`
}

// KBSyncResult is the answer of both knowledge-base tools.
type KBSyncResult struct {
	JobID string       `json:"jobId"`
	Pages []KBSyncPage `json:"pages"`
}

// registerYouTrackTools declares the YouTrack half of the surface.
func registerYouTrackTools(s *Server) {
	register(s, toolDef{
		Name:  "import_youtrack_issues",
		Title: "Import YouTrack issues",
		Description: "Import issues from the linked YouTrack project as backlog items, by query or " +
			"by readable id. An issue an item already carries is updated in place and keeps its " +
			"git-in-track id, so re-importing the same issue never produces a second item. " +
			"Set dryRun to see the plan without writing anything.",
		Write:     true,
		Untrusted: true,
	}, importYouTrackIssues)

	register(s, toolDef{
		Name:  "push_comment_to_youtrack",
		Title: "Push comments to YouTrack",
		Description: "Queue one comment, or every comment of an item, for the YouTrack issue the " +
			"item mirrors. The push runs as a background job: the tool returns the job id rather " +
			"than waiting on the tracker. An item that carries no YouTrack reference is refused — " +
			"import or link it first.",
		Write: true,
	}, pushCommentToYouTrack)

	register(s, toolDef{
		Name:  "publish_kb_page_to_youtrack",
		Title: "Publish a knowledge-base page",
		Description: "Queue a knowledge-base page, or a whole folder, for publication as YouTrack " +
			"articles. The `## Feedback` block never leaves the repository. The work runs as a " +
			"background job: the tool returns the job id.",
		Write: true,
	}, publishKBPage)

	register(s, toolDef{
		Name:  "sync_kb_page_from_youtrack",
		Title: "Pull a knowledge-base page",
		Description: "Queue writing a knowledge-base page, or a whole folder, back from the " +
			"YouTrack articles it mirrors. The local feedback block is preserved, and a page both " +
			"sides changed produces a `.conflict.md` beside it rather than a merge. The work runs " +
			"as a background job: the tool returns the job id.",
		Write: true,
	}, pullKBPage)
}

// ---------------------------------------------------------------- handlers --

// importYouTrackIssues runs one import, or previews it. dryRun picks the method:
// the two share every step up to the writes, so a preview cannot disagree with
// the run it precedes.
func importYouTrackIssues(
	ctx context.Context, s *Server, in ImportYouTrackIssuesInput,
) (ImportResult, error) {
	query := strings.TrimSpace(in.Query)
	ids := trimmedList(in.IDs)
	switch {
	case query == "" && len(ids) == 0:
		return ImportResult{}, invalidField("query",
			"name the issues to import, by query or by readable id",
			"State: Open #Unresolved")
	case query != "" && len(ids) > 0:
		return ImportResult{}, invalidField("query",
			"give a query or a list of ids, not both", "ACME-42")
	}

	params := map[string]any{
		"project": in.Project, "query": query, "ids": ids, "depth": in.Depth,
		"includeLinks": in.IncludeLinks, "includeComments": in.IncludeComments,
		"includeAttachments": in.IncludeAttachments,
	}
	if in.DryRun {
		preview, err := dispatch[struct {
			Project string `json:"project"`
			Issues  []struct {
				YouTrackID string    `json:"youtrackId"`
				Title      string    `json:"title"`
				Action     string    `json:"action"`
				TargetID   string    `json:"targetId"`
				Comments   int       `json:"comments"`
				Warnings   []warning `json:"warnings"`
			} `json:"issues"`
			Warnings []warning `json:"warnings"`
		}](ctx, s, "youtrack.import.preview", params)
		if err != nil {
			return ImportResult{}, err
		}
		out := ImportResult{
			Project: preview.Project, DryRun: true,
			Issues:   make([]ImportedIssue, 0, len(preview.Issues)),
			Warnings: warningLines(preview.Warnings),
		}
		for _, issue := range preview.Issues {
			entry := ImportedIssue{
				YouTrackID: issue.YouTrackID, Title: issue.Title, Action: issue.Action,
				ItemID: issue.TargetID, Comments: issue.Comments,
				Warnings: warningLines(issue.Warnings),
			}
			entry.Rev, _ = s.itemRev(ctx, entry.ItemID)
			out.Issues = append(out.Issues, entry)
			if issue.Action == "update" {
				out.Updated++
			} else {
				out.Created++
			}
		}
		return out, nil
	}

	result, err := s.dispatchRaw(ctx, "youtrack.import.run", params)
	if err != nil {
		return ImportResult{}, err
	}
	payload, err := decodeResult[struct {
		Project string `json:"project"`
		Issues  []struct {
			YouTrackID string    `json:"youtrackId"`
			ItemID     string    `json:"itemId"`
			Action     string    `json:"action"`
			Comments   int       `json:"comments"`
			Warnings   []warning `json:"warnings"`
			Error      string    `json:"error"`
		} `json:"issues"`
		Created  int       `json:"created"`
		Updated  int       `json:"updated"`
		Failed   int       `json:"failed"`
		Warnings []warning `json:"warnings"`
		Writes   writeSet  `json:"writes"`
	}](result)
	if err != nil {
		return ImportResult{}, err
	}

	out := ImportResult{
		Project: payload.Project,
		Issues:  make([]ImportedIssue, 0, len(payload.Issues)),
		Created: payload.Created, Updated: payload.Updated, Failed: payload.Failed,
		Changed: payload.Writes.paths(), Warnings: warningLines(payload.Warnings),
	}
	for _, issue := range payload.Issues {
		entry := ImportedIssue{
			YouTrackID: issue.YouTrackID, Action: issue.Action, ItemID: issue.ItemID,
			Comments: issue.Comments, Warnings: warningLines(issue.Warnings),
			Error: issue.Error,
		}
		// Every item an agent may write next carries the rev that write has to
		// quote, so a caller never has to read the item back first.
		entry.Rev, _ = s.itemRev(ctx, entry.ItemID)
		out.Issues = append(out.Issues, entry)
	}
	s.announce(ctx, WriteEvent{
		Tool: "import_youtrack_issues", Method: "youtrack.import.run",
		Op: "updated", Result: result,
	})
	return out, nil
}

// pushCommentToYouTrack queues the comments of one item for the tracker.
func pushCommentToYouTrack(
	ctx context.Context, s *Server, in PushCommentInput,
) (PushCommentResult, error) {
	item := strings.TrimSpace(in.ItemID)
	if item == "" {
		return PushCommentResult{}, invalidField("itemId",
			"name the item whose comments are pushed", "ACME-US-0042")
	}
	path := strings.TrimSpace(in.CommentPath)
	switch {
	case path == "" && !in.All:
		return PushCommentResult{}, invalidField("commentPath",
			"push one comment by path, or every comment of the item with all",
			"docs/.pmngr/comments/ACME-US-0042/20260903T142500Z-claude.md")
	case path != "" && in.All:
		return PushCommentResult{}, invalidField("commentPath",
			"give a comment path or all, not both", true)
	}
	if path != "" {
		// A comment path is user-supplied, so it goes through the guard before
		// it reaches the core, exactly as a knowledge-base path does.
		clean, err := s.guard.Check("commentPath", path)
		if err != nil {
			return PushCommentResult{}, err
		}
		path = clean
	}

	result, err := s.dispatchRaw(ctx, "youtrack.comment.push", map[string]any{
		"project": in.Project, "itemId": item, "commentPath": path, "all": in.All,
	})
	if err != nil {
		return PushCommentResult{}, err
	}
	payload, err := decodeResult[struct {
		ItemID  string          `json:"itemId"`
		JobID   string          `json:"jobId"`
		Pushed  []PushedComment `json:"pushed"`
		Skipped []PushedComment `json:"skipped"`
		Failed  []PushedComment `json:"failed"`
	}](result)
	if err != nil {
		return PushCommentResult{}, err
	}
	out := PushCommentResult{
		ItemID: payload.ItemID, JobID: payload.JobID,
		Pushed: emptyComments(payload.Pushed), Skipped: emptyComments(payload.Skipped),
		Failed: emptyComments(payload.Failed),
	}
	s.announce(ctx, WriteEvent{
		Tool: "push_comment_to_youtrack", Method: "youtrack.comment.push",
		ItemID: item, Op: "updated", Result: result,
	})
	return out, nil
}

// publishKBPage queues a publication.
func publishKBPage(ctx context.Context, s *Server, in KBSyncInput) (KBSyncResult, error) {
	return kbSync(ctx, s, "publish_kb_page_to_youtrack", "youtrack.kb.publish", "publish", in)
}

// pullKBPage queues a pull.
func pullKBPage(ctx context.Context, s *Server, in KBSyncInput) (KBSyncResult, error) {
	return kbSync(ctx, s, "sync_kb_page_from_youtrack", "youtrack.kb.pull", "pull", in)
}

// kbSync is the shared half of the two knowledge-base tools: guard the path,
// dispatch, project.
func kbSync(
	ctx context.Context, s *Server, tool, method, action string, in KBSyncInput,
) (KBSyncResult, error) {
	if strings.TrimSpace(in.Path) == "" {
		return KBSyncResult{}, invalidField("path",
			"name the page or the folder to synchronize",
			"docs/architecture/auth.md")
	}
	// The guard is what keeps a user-supplied path inside the mounted roots; it
	// runs before the core is asked anything.
	clean, err := s.guard.Check("path", in.Path)
	if err != nil {
		return KBSyncResult{}, err
	}

	result, err := s.dispatchRaw(ctx, method, map[string]any{
		"project": in.Project, "path": clean, "recursive": in.Recursive,
	})
	if err != nil {
		return KBSyncResult{}, err
	}
	payload, err := decodeResult[struct {
		JobID string   `json:"jobId"`
		Pages []string `json:"pages"`
	}](result)
	if err != nil {
		return KBSyncResult{}, err
	}
	out := KBSyncResult{JobID: payload.JobID, Pages: make([]KBSyncPage, 0, len(payload.Pages))}
	for _, page := range payload.Pages {
		out.Pages = append(out.Pages, KBSyncPage{Path: page, Action: action})
	}
	s.announce(ctx, WriteEvent{
		Tool: tool, Method: method, Op: "updated", Result: result,
	})
	return out, nil
}

// ----------------------------------------------------------------- helpers --

// warning is one finding of the mapping, decoded loosely so that a field added
// on the core side does not break this package.
type warning struct {
	Field    string `json:"field"`
	Value    string `json:"value"`
	Fallback string `json:"fallback"`
	Reason   string `json:"reason"`
}

// warningLines renders warnings as the one-line strings a model reads, keeping
// the field and the value that identify what could not be translated.
func warningLines(ws []warning) []string {
	if len(ws) == 0 {
		return nil
	}
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		line := w.Reason
		switch {
		case w.Field != "" && w.Value != "":
			line = w.Field + " " + w.Value + ": " + w.Reason
		case w.Field != "":
			line = w.Field + ": " + w.Reason
		}
		out = append(out, line)
	}
	return out
}

// trimmedList drops the blank entries of a repeated string argument.
func trimmedList(raw []string) []string {
	out := make([]string, 0, len(raw))
	for _, value := range raw {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// emptyComments turns a nil list into an empty one, so that the three lists of
// a push result are always present and a client never has to distinguish
// "absent" from "none".
func emptyComments(list []PushedComment) []PushedComment {
	if list == nil {
		return []PushedComment{}
	}
	return list
}
