package feed

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/angelospanag/riffle/internal/db"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// CreateInput represents the input for creating a feed
type CreateInput struct {
	Body Input
}

// CreateOutput represents the output for creating a feed
type CreateOutput struct {
	Body FeedResponse
}

// ListOutput represents the output for listing feeds
type ListOutput struct {
	Body []FeedResponse
}

// GetInput represents the input for getting a feed
type GetInput struct {
	FeedID int32 `path:"id" minimum:"1" example:"1"`
}

// GetOutput represents the output for getting a feed
type GetOutput struct {
	Body FeedResponse
}

// UpdateInput represents the input for updating a feed
type UpdateInput struct {
	FeedID int32 `path:"id" minimum:"1" example:"1"`
	Body   Input
}

// UpdateOutput represents the output for updating a feed
type UpdateOutput struct {
	Body FeedResponse
}

// DeleteInput represents the input for deleting a feed
type DeleteInput struct {
	FeedID int32 `path:"id" minimum:"1" example:"1"`
}

// RefreshInput represents the input for refreshing feeds
type RefreshInput struct {
	Body RefreshRequest
}

// RefreshOutput represents the output for refreshing feeds
type RefreshOutput struct {
	Body RefreshResponse
}

// Helper function to map database feed to response
func mapToResponse(feed db.Feed) FeedResponse {
	var description, link, faviconURL *string
	var lastFetchedAt *time.Time

	if feed.Description.Valid {
		description = &feed.Description.String
	}
	if feed.Link.Valid {
		link = &feed.Link.String
	}
	if feed.FaviconUrl.Valid {
		faviconURL = &feed.FaviconUrl.String
	}
	if feed.LastFetchedAt.Valid {
		lastFetchedAt = &feed.LastFetchedAt.Time
	}

	return FeedResponse{
		ID:            feed.ID,
		Title:         feed.Title,
		URL:           feed.Url,
		Description:   description,
		Link:          link,
		FaviconURL:    faviconURL,
		LastFetchedAt: lastFetchedAt,
		CreatedAt:     feed.CreatedAt.Time,
		UpdatedAt:     feed.UpdatedAt.Time,
	}
}

// RegisterRoutes initializes the feed service and registers all feed routes
func RegisterRoutes(api huma.API, querier db.Querier, logger *slog.Logger) {
	service := NewService(querier, logger)

	// Register all feed-related routes with the Huma API
	huma.Register(api, huma.Operation{
		OperationID:   "create-feed",
		Method:        http.MethodPost,
		Path:          "/feeds",
		Summary:       "Create a new RSS feed",
		Description:   "Add a new RSS feed to follow",
		Tags:          []string{"feeds"},
		DefaultStatus: http.StatusCreated,
	}, func(ctx context.Context, input *CreateInput) (*CreateOutput, error) {
		logger := ctx.Value("logger").(*slog.Logger)
		logger.Info("creating feed", "url", input.Body.URL)

		if err := service.ValidateFeed(ctx, input.Body.URL); err != nil {
			logger.Warn("feed URL failed validation", "url", input.Body.URL, "error", err)
			return nil, huma.Error422UnprocessableEntity(
				"could not fetch or parse a feed at this URL",
				err,
			)
		}

		feed, err := service.GetQueries().CreateFeed(ctx, db.CreateFeedParams{
			Title: input.Body.Title,
			Url:   input.Body.URL,
			Description: pgtype.Text(sql.NullString{
				String: input.Body.Description,
				Valid:  input.Body.Description != "",
			}),
			Link: pgtype.Text(sql.NullString{
				String: input.Body.Link,
				Valid:  input.Body.Link != "",
			}),
		})
		if err != nil {
			logger.Error("failed to create feed", "error", err)
			return nil, huma.Error409Conflict("feed with this URL already exists")
		}

		return &CreateOutput{
			Body: mapToResponse(feed),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-feeds",
		Method:      http.MethodGet,
		Path:        "/feeds",
		Summary:     "List all feeds",
		Description: "Retrieve all RSS feeds",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *struct{}) (*ListOutput, error) {
		logger := ctx.Value("logger").(*slog.Logger)
		logger.Info("listing feeds")

		feeds, err := service.GetQueries().ListFeeds(ctx)
		if err != nil {
			logger.Error("failed to list feeds", "error", err)
			return nil, huma.Error500InternalServerError("failed to retrieve feeds")
		}

		response := make([]FeedResponse, len(feeds))
		for i, feed := range feeds {
			response[i] = mapToResponse(feed)
		}

		return &ListOutput{Body: response}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-feed",
		Method:      http.MethodGet,
		Path:        "/feeds/{id}",
		Summary:     "Get a feed",
		Description: "Retrieve a specific RSS feed by ID",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *GetInput) (*GetOutput, error) {
		logger := ctx.Value("logger").(*slog.Logger)
		logger.Info("getting feed", "feed_id", input.FeedID)

		feed, err := service.GetQueries().GetFeed(ctx, input.FeedID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, huma.Error404NotFound("feed not found")
			}
			logger.Error("failed to get feed", "error", err)
			return nil, huma.Error500InternalServerError("failed to retrieve feed")
		}

		return &GetOutput{
			Body: mapToResponse(feed),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-feed",
		Method:      http.MethodPut,
		Path:        "/feeds/{id}",
		Summary:     "Update a feed",
		Description: "Update an existing RSS feed",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *UpdateInput) (*UpdateOutput, error) {
		logger := ctx.Value("logger").(*slog.Logger)
		logger.Info("updating feed", "feed_id", input.FeedID)

		feed, err := service.GetQueries().UpdateFeed(ctx, db.UpdateFeedParams{
			ID:    input.FeedID,
			Title: input.Body.Title,
			Description: pgtype.Text(sql.NullString{
				String: input.Body.Description,
				Valid:  input.Body.Description != "",
			}),
			Link: pgtype.Text(sql.NullString{
				String: input.Body.Link,
				Valid:  input.Body.Link != "",
			}),
		})
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, huma.Error404NotFound("feed not found")
			}
			logger.Error("failed to update feed", "error", err)
			return nil, huma.Error500InternalServerError("failed to update feed")
		}

		return &UpdateOutput{
			Body: mapToResponse(feed),
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-feed",
		Method:      http.MethodDelete,
		Path:        "/feeds/{id}",
		Summary:     "Delete a feed",
		Description: "Remove an RSS feed and all its posts",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *DeleteInput) (*struct{}, error) {
		logger := ctx.Value("logger").(*slog.Logger)
		logger.Info("deleting feed", "feed_id", input.FeedID)

		err := service.GetQueries().DeleteFeed(ctx, input.FeedID)
		if err != nil {
			logger.Error("failed to delete feed", "error", err)
			return nil, huma.Error500InternalServerError("failed to delete feed")
		}

		return nil, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "refresh-feeds",
		Method:      http.MethodPost,
		Path:        "/feeds/refresh",
		Summary:     "Refresh feeds",
		Description: "Fetch new posts from all feeds or a specific feed",
		Tags:        []string{"feeds"},
	}, func(ctx context.Context, input *RefreshInput) (*RefreshOutput, error) {
		logger := ctx.Value("logger").(*slog.Logger)
		logger.Info("refreshing feeds")

		var feedsUpdated, postsAdded int
		var err error

		if input.Body.FeedID != nil {
			postsAdded, err = service.RefreshFeed(ctx, *input.Body.FeedID)
			if err != nil {
				logger.Error("failed to refresh feed", "error", err)
				return nil, huma.Error500InternalServerError("failed to refresh feed")
			}
			feedsUpdated = 1
		} else {
			feedsUpdated, postsAdded, err = service.RefreshAllFeeds(ctx)
			if err != nil {
				logger.Error("failed to refresh feeds", "error", err)
				return nil, huma.Error500InternalServerError("failed to refresh feeds")
			}
		}

		return &RefreshOutput{
			Body: RefreshResponse{
				Message:      "Feeds refreshed successfully",
				FeedsUpdated: feedsUpdated,
				PostsAdded:   postsAdded,
			},
		}, nil
	})
}
