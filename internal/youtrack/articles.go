package youtrack

import (
	"context"
	"fmt"
	"net/url"
)

// Field selectors for the knowledge-base endpoints.
const (
	// ArticleFields is the full article shape. The body of an article is
	// "content", not "description" as on an issue.
	ArticleFields = "id,idReadable,summary,content,ordinal,created,updated,hasChildren," +
		"project(id,name,shortName)," +
		"parentArticle(id,idReadable,summary)," +
		"reporter(id,login,fullName,email)"
	// ChildArticleFields is the narrow shape used to walk the tree downwards.
	ChildArticleFields = "id,idReadable,summary,ordinal,hasChildren"
	// ArticleListFields is the narrow shape used while paging a search.
	ArticleListFields = "id,idReadable,summary,updated,project(shortName)"
)

// ArticleInput is the body of a create or an update. Every field is a pointer
// so an update sends only what the caller actually set: YouTrack merges the
// keys present in the body and leaves the rest alone. ProjectID is accepted
// only on a create, because the project of an article is read-only afterwards;
// re-parenting is done through ParentArticleID.
type ArticleInput struct {
	Summary         *string
	Content         *string
	ProjectID       string
	ParentArticleID *string
}

// payload renders the input as the JSON body YouTrack expects. includeProject
// is true only on a create.
func (in ArticleInput) payload(includeProject bool) map[string]any {
	body := map[string]any{}
	if in.Summary != nil {
		body["summary"] = *in.Summary
	}
	if in.Content != nil {
		body["content"] = *in.Content
	}
	if includeProject && in.ProjectID != "" {
		body["project"] = map[string]any{"id": in.ProjectID}
	}
	if in.ParentArticleID != nil {
		if *in.ParentArticleID == "" {
			body["parentArticle"] = nil
		} else {
			body["parentArticle"] = map[string]any{"id": *in.ParentArticleID}
		}
	}
	return body
}

// Article reads one knowledge-base article. id accepts a readable id such as
// "ACME-A-3" — the "-A-" infix is what distinguishes an article id from an
// issue id — or an internal id such as "42-7". Content is untrusted
// third-party Markdown: sanitize it before rendering.
func (c *Client) Article(ctx context.Context, id string) (Article, error) {
	if id == "" {
		return Article{}, fmt.Errorf("%w: article id is empty", ErrInvalidInput)
	}
	var out Article
	path := "/api/articles/" + url.PathEscape(id)
	if err := c.get(ctx, path, fieldsQuery(ArticleFields, nil), &out); err != nil {
		return Article{}, err
	}
	return out, nil
}

// CreateArticle creates an article. A project is mandatory and is validated
// locally, before any request is made.
func (c *Client) CreateArticle(ctx context.Context, in ArticleInput) (Article, error) {
	if in.ProjectID == "" {
		return Article{}, fmt.Errorf("%w: creating an article needs a project id", ErrInvalidInput)
	}
	if in.Summary == nil || *in.Summary == "" {
		return Article{}, fmt.Errorf("%w: creating an article needs a summary", ErrInvalidInput)
	}
	var out Article
	if err := c.post(ctx, "/api/articles", fieldsQuery(ArticleFields, nil), in.payload(true), &out); err != nil {
		return Article{}, err
	}
	return out, nil
}

// UpdateArticle applies a partial update. YouTrack has no PUT: an update is a
// POST to the resource carrying only the keys that change.
func (c *Client) UpdateArticle(ctx context.Context, id string, in ArticleInput) (Article, error) {
	if id == "" {
		return Article{}, fmt.Errorf("%w: article id is empty", ErrInvalidInput)
	}
	body := in.payload(false)
	if len(body) == 0 {
		return Article{}, fmt.Errorf("%w: updating an article needs at least one field", ErrInvalidInput)
	}
	var out Article
	path := "/api/articles/" + url.PathEscape(id)
	if err := c.post(ctx, path, fieldsQuery(ArticleFields, nil), body, &out); err != nil {
		return Article{}, err
	}
	return out, nil
}

// ChildArticles lists the direct children of an article, which is how the
// knowledge-base tree is walked downwards; an article itself only carries its
// parent.
func (c *Client) ChildArticles(ctx context.Context, id string) ([]ArticleRef, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: article id is empty", ErrInvalidInput)
	}
	var out []ArticleRef
	path := "/api/articles/" + url.PathEscape(id) + "/childArticles"
	if err := c.get(ctx, path, fieldsQuery(ChildArticleFields, Page{}.values()), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SearchArticles runs one page of an article search. The query goes through
// EnsureOrderBy for the same reason issue searches do: the reference client
// pages articles without an ordering clause, which loses rows.
func (c *Client) SearchArticles(ctx context.Context, query string, page Page) ([]Article, error) {
	params := page.values()
	params.Set("query", EnsureOrderBy(query))
	var out []Article
	if err := c.get(ctx, "/api/articles", fieldsQuery(ArticleListFields, params), &out); err != nil {
		return nil, err
	}
	return out, nil
}

// SearchAllArticles walks every page of an article search.
func (c *Client) SearchAllArticles(ctx context.Context, query string, page Page) ([]Article, error) {
	return walkAll(ctx, page, func(ctx context.Context, p Page) ([]Article, error) {
		return c.SearchArticles(ctx, query, p)
	})
}

// ArticleAttachments lists the files attached to an article.
func (c *Client) ArticleAttachments(ctx context.Context, id string) ([]Attachment, error) {
	if id == "" {
		return nil, fmt.Errorf("%w: article id is empty", ErrInvalidInput)
	}
	var out []Attachment
	path := "/api/articles/" + url.PathEscape(id) + "/attachments"
	if err := c.get(ctx, path, fieldsQuery(AttachmentFields, Page{}.values()), &out); err != nil {
		return nil, err
	}
	return out, nil
}
