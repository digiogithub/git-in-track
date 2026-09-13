package youtrack

import (
	"context"
	"fmt"
	"net/url"
)

// Field selectors for the issue endpoints.
const (
	// IssueFields is the wide selector: everything a mapping layer needs from
	// one issue in a single request. It asks for "$type" on custom fields and
	// their values, which the reference client omits and which a typed caller
	// needs to know how to write the field back.
	IssueFields = "id,idReadable,summary,description,created,updated,resolved," +
		"project(id,name,shortName)," +
		"reporter(id,login,fullName,email)," +
		"customFields(id,name,$type,value(id,name,$type,login,fullName,localizedName,presentation,minutes,isResolved,idReadable,text))," +
		"links(id,direction,linkType(id,name,sourceToTarget,targetToSource,directed,aggregation),issues(id,idReadable,summary))," +
		"tags(id,name)"
	// IssueListFields is the narrow selector used while paging a search.
	IssueListFields = "id,idReadable,summary,updated,resolved,project(shortName)"
	// IssueLinkFields is the selector of the dedicated links sub-resource.
	IssueLinkFields = "id,direction,linkType(id,name,sourceToTarget,targetToSource,directed,aggregation)," +
		"issues(id,idReadable,summary)"
	// CommentFields is the comment shape this package requests.
	CommentFields = "id,text,created,updated,author(id,login,fullName,email)"
	// AttachmentFields is the attachment shape this package requests.
	AttachmentFields = "id,name,size,mimeType,extension,charset,created,url,author(login)"
)

// SearchIssues runs one page of an issue search. The query goes through
// EnsureOrderBy, so a $skip walk is always ordered and never loses or repeats a
// row. Pass a non-empty fields selector through SearchIssuesWithFields when the
// narrow list shape is not enough.
func (c *Client) SearchIssues(ctx context.Context, query string, page Page) ([]Issue, error) {
	return c.SearchIssuesWithFields(ctx, query, page, IssueListFields)
}

// SearchIssuesWithFields is SearchIssues with an explicit field selector, for
// callers that want the wide shape while paging.
func (c *Client) SearchIssuesWithFields(ctx context.Context, query string, page Page, fields string) ([]Issue, error) {
	params := page.values()
	params.Set("query", EnsureOrderBy(query))
	var out []Issue
	if err := c.get(ctx, "/api/issues", fieldsQuery(fields, params), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SearchAllIssues walks every page of a search, stopping on a short page.
func (c *Client) SearchAllIssues(ctx context.Context, query string, page Page) ([]Issue, error) {
	return walkAll(ctx, page, func(ctx context.Context, p Page) ([]Issue, error) {
		return c.SearchIssues(ctx, query, p)
	})
}

// Issue reads one issue with the wide selector. id accepts a readable id such
// as "ACME-42" or an internal id such as "2-345". The returned Description is
// untrusted third-party Markdown.
func (c *Client) Issue(ctx context.Context, id string) (Issue, error) {
	if id == "" {
		return Issue{}, fmt.Errorf("%w: issue id is empty", ErrInvalidInput)
	}
	var out Issue
	path := "/api/issues/" + url.PathEscape(id)
	if err := c.get(ctx, path, fieldsQuery(IssueFields, nil), &out); err != nil {
		return Issue{}, err
	}
	return out, nil
}

// IssueLinks reads the link graph of one issue. YouTrack returns one entry per
// (linkType, direction) pair including the empty ones, so the result is passed
// through NonEmptyLinks before it is returned.
//
// Reading rule: linkType "Subtask" with direction OUTWARD lists the children of
// the issue, INWARD names its parent, and BOTH is used by undirected types such
// as "Relates".
func (c *Client) IssueLinks(ctx context.Context, id string) ([]IssueLink, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: issue id is empty", ErrInvalidInput)
	}
	var out []IssueLink
	path := "/api/issues/" + url.PathEscape(id) + "/links"
	if err := c.get(ctx, path, fieldsQuery(IssueLinkFields, nil), &out); err != nil {
		return nil, err
	}
	return NonEmptyLinks(out), nil
}

// Comments reads one page of an issue's comments. $top is always sent: the
// endpoint truncates at the server default otherwise, silently.
func (c *Client) Comments(ctx context.Context, id string, page Page) ([]Comment, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: issue id is empty", ErrInvalidInput)
	}
	var out []Comment
	path := "/api/issues/" + url.PathEscape(id) + "/comments"
	if err := c.get(ctx, path, fieldsQuery(CommentFields, page.values()), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// AllComments walks every page of an issue's comments. The sub-resource is
// returned in creation order by the server, so no query ordering applies here.
func (c *Client) AllComments(ctx context.Context, id string) ([]Comment, error) {
	return walkAll(ctx, Page{}, func(ctx context.Context, page Page) ([]Comment, error) {
		return c.Comments(ctx, id, page)
	})
}

// AddComment posts a comment and returns the created one. text is Markdown.
func (c *Client) AddComment(ctx context.Context, id, text string) (Comment, error) {
	if id == "" {
		return Comment{}, fmt.Errorf("%w: issue id is empty", ErrInvalidInput)
	}
	if text == "" {
		return Comment{}, fmt.Errorf("%w: comment text is empty", ErrInvalidInput)
	}
	body := map[string]any{"text": text}
	var out Comment
	path := "/api/issues/" + url.PathEscape(id) + "/comments"
	if err := c.post(ctx, path, fieldsQuery(CommentFields, nil), body, &out); err != nil {
		return Comment{}, err
	}
	return out, nil
}

// UpdateComment edits a comment that was already posted and returns the
// comment as the server stored it. text is Markdown and replaces the whole
// body; YouTrack has no partial comment update.
//
// The verb is POST, not PUT: YouTrack addresses an existing comment with the
// same method it creates one with, the id in the path being the whole of the
// difference. That is why this package has no put helper.
//
// This is what makes a comment push idempotent. A caller that recorded the
// remote id of a comment it created edits that comment on a re-delivery
// instead of posting a second copy of it.
func (c *Client) UpdateComment(ctx context.Context, id, commentID, text string) (Comment, error) {
	if id == "" {
		return Comment{}, fmt.Errorf("%w: issue id is empty", ErrInvalidInput)
	}
	if commentID == "" {
		return Comment{}, fmt.Errorf("%w: comment id is empty", ErrInvalidInput)
	}
	if text == "" {
		// An empty text would clear the comment rather than edit it, and no
		// caller of this package means that; deleting a remote comment is a
		// deliberate act, not the result of an empty string.
		return Comment{}, fmt.Errorf("%w: comment text is empty", ErrInvalidInput)
	}
	body := map[string]any{"text": text}
	var out Comment
	path := "/api/issues/" + url.PathEscape(id) + "/comments/" + url.PathEscape(commentID)
	if err := c.post(ctx, path, fieldsQuery(CommentFields, nil), body, &out); err != nil {
		return Comment{}, err
	}
	return out, nil
}

// Attachments lists the files attached to an issue. $top is always sent, for
// the same reason as on comments.
func (c *Client) Attachments(ctx context.Context, id string) ([]Attachment, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: issue id is empty", ErrInvalidInput)
	}
	var out []Attachment
	path := "/api/issues/" + url.PathEscape(id) + "/attachments"
	if err := c.get(ctx, path, fieldsQuery(AttachmentFields, Page{}.values()), &out); err != nil {
		return nil, err
	}
	return out, nil
}
