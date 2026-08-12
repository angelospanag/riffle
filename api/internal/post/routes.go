package post

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/angelospanag/riffle/internal/db"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
)

// RegisterRoutes initializes the post service and registers all post routes
func RegisterRoutes(api huma.API, querier db.Querier, logger *slog.Logger) {
	service := NewService(querier, logger)

	huma.Register(api, huma.Operation{
		OperationID: "mark-all-read",
		Method:      http.MethodPost,
		Path:        "/feeds/{id}/mark-all-read",
		Summary:     "Mark all posts as read",
		Description: "Mark all posts in a feed as read",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *MarkAllReadInput) (*struct{}, error) {
		logger := ctx.Value("logger").(*slog.Logger)
		logger.Info("marking all posts as read", "feed_id", input.FeedID)

		err := service.MarkAllPostsAsRead(ctx, input.FeedID)
		if err != nil {
			logger.Error("failed to mark all posts as read", "error", err)
			return nil, huma.Error500InternalServerError("failed to update posts")
		}

		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "count-unread-by-feed",
		Method:      http.MethodGet,
		Path:        "/feeds/{id}/unread/count",
		Summary:     "Count unread posts by feed",
		Description: "Get the number of unread posts for a specific feed",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *CountUnreadByFeedInput) (*CountUnreadOutput, error) {
		logger := ctx.Value("logger").(*slog.Logger)
		logger.Info("counting unread posts by feed", "feed_id", input.FeedID)

		count, err := service.CountUnreadPostsByFeed(ctx, input.FeedID)
		if err != nil {
			logger.Error("failed to count unread posts", "error", err)
			return nil, huma.Error500InternalServerError("failed to count posts")
		}

		return &CountUnreadOutput{
			Body: struct {
				Count int64 `json:"count" example:"42"`
			}{Count: count},
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-posts",
		Method:      http.MethodGet,
		Path:        "/posts",
		Summary:     "List posts",
		Description: "Retrieve posts with optional filters (feed_id, unread_only)",
		Tags:        []string{"posts"},
	}, func(ctx context.Context, input *ListInput) (*ListOutput, error) {
		logger := ctx.Value("logger").(*slog.Logger)
		logger.Info("listing posts",
			"limit", input.Limit,
			"offset", input.Offset,
			"feed_id", input.FeedID,
			"unread_only", input.UnreadOnly)

		var posts []db.Post
		var err error

		if input.FeedID > 0 {
			// List posts for specific feed
			if input.UnreadOnly {
				posts, err = service.ListUnreadPostsByFeed(ctx, db.ListUnreadPostsByFeedParams{
					FeedID: input.FeedID,
					Limit:  input.Limit,
					Offset: input.Offset,
				})
			} else {
				posts, err = service.ListPostsByFeed(ctx, db.ListPostsByFeedParams{
					FeedID: input.FeedID,
					Limit:  input.Limit,
					Offset: input.Offset,
				})
			}
		} else {
			// List all posts
			if input.UnreadOnly {
				posts, err = service.ListUnreadPosts(ctx, db.ListUnreadPostsParams{
					Limit:  input.Limit,
					Offset: input.Offset,
				})
			} else {
				posts, err = service.ListPosts(ctx, db.ListPostsParams{
					Limit:  input.Limit,
					Offset: input.Offset,
				})
			}
		}

		if err != nil {
			logger.Error("failed to list posts", "error", err)
			return nil, huma.Error500InternalServerError("failed to retrieve posts")
		}

		response := make([]PostResponse, len(posts))
		for i, post := range posts {
			response[i] = mapToResponse(post)
		}

		return &ListOutput{Body: response}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-post",
		Method:      http.MethodGet,
		Path:        "/posts/{id}",
		Summary:     "Get a post",
		Description: "Retrieve a specific post by ID",
		Tags:        []string{"posts"},
	}, func(ctx context.Context, input *GetInput) (*GetOutput, error) {
		logger := ctx.Value("logger").(*slog.Logger)
		logger.Info("getting post", "post_id", input.PostID)

		post, err := service.GetPost(ctx, input.PostID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, huma.Error404NotFound("post not found")
			}
			logger.Error("failed to get post", "error", err)
			return nil, huma.Error500InternalServerError("failed to retrieve post")
		}

		return &GetOutput{
			Body: mapToResponse(post),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "mark-post-read",
		Method:      http.MethodPatch,
		Path:        "/posts/{id}/read",
		Summary:     "Mark post as read/unread",
		Description: "Update the read status of a post",
		Tags:        []string{"posts"},
	}, func(ctx context.Context, input *MarkReadInput) (*struct{}, error) {
		logger := ctx.Value("logger").(*slog.Logger)
		logger.Info("marking post", "post_id", input.PostID, "is_read", input.Body.IsRead)

		var err error
		if input.Body.IsRead {
			err = service.MarkPostAsRead(ctx, input.PostID)
		} else {
			err = service.MarkPostAsUnread(ctx, input.PostID)
		}

		if err != nil {
			logger.Error("failed to mark post", "error", err)
			return nil, huma.Error500InternalServerError("failed to update post")
		}

		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "count-unread",
		Method:      http.MethodGet,
		Path:        "/posts/unread/count",
		Summary:     "Count unread posts",
		Description: "Get the total number of unread posts",
		Tags:        []string{"posts"},
	}, func(ctx context.Context, input *struct{}) (*CountUnreadOutput, error) {
		logger := ctx.Value("logger").(*slog.Logger)
		logger.Info("counting unread posts")

		count, err := service.CountUnreadPosts(ctx)
		if err != nil {
			logger.Error("failed to count unread posts", "error", err)
			return nil, huma.Error500InternalServerError("failed to count posts")
		}

		return &CountUnreadOutput{
			Body: struct {
				Count int64 `json:"count" example:"42"`
			}{Count: count},
		}, nil
	})
}

// ListInput represents the input for listing posts
type ListInput struct {
	ListParamsWithFilters
}

// ListOutput represents the output for listing posts
type ListOutput struct {
	Body []PostResponse
}

// GetInput represents the input for getting a post
type GetInput struct {
	PostID int32 `path:"id" minimum:"1" example:"1"`
}

// GetOutput represents the output for getting a post
type GetOutput struct {
	Body PostResponse
}

// MarkReadInput represents the input for marking a post as read/unread
type MarkReadInput struct {
	PostID int32 `path:"id" minimum:"1" example:"1"`
	Body   MarkReadRequest
}

// MarkAllReadInput represents the input for marking all posts in a feed as read
type MarkAllReadInput struct {
	FeedID int32 `path:"id" minimum:"1" example:"1"`
}

// CountUnreadOutput represents the output for counting unread posts
type CountUnreadOutput struct {
	Body struct {
		Count int64 `json:"count" example:"42"`
	}
}

// CountUnreadByFeedInput represents the input for counting unread posts by feed
type CountUnreadByFeedInput struct {
	FeedID int32 `path:"id" minimum:"1" example:"1"`
}

// Helper function to map database post to response
func mapToResponse(post db.Post) PostResponse {
	var description, content, author *string
	var publishedAt *time.Time

	if post.Description.Valid {
		description = &post.Description.String
	}
	if post.Content.Valid {
		content = &post.Content.String
	}
	if post.Author.Valid {
		author = &post.Author.String
	}
	if post.PublishedAt.Valid {
		publishedAt = &post.PublishedAt.Time
	}

	return PostResponse{
		ID:          post.ID,
		FeedID:      post.FeedID,
		Title:       post.Title,
		Description: description,
		Content:     content,
		Link:        post.Link,
		Author:      author,
		PublishedAt: publishedAt,
		GUID:        post.Guid,
		IsRead:      post.IsRead,
		CreatedAt:   post.CreatedAt.Time,
		UpdatedAt:   post.UpdatedAt.Time,
	}
}
