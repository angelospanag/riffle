package feed

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/angelospanag/riffle/internal/db"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/mmcdole/gofeed"
)

// FeedParser fetches and parses a feed URL. Satisfied by *gofeed.Parser;
// exists so tests can substitute a stub instead of hitting the network.
type FeedParser interface {
	ParseURLWithContext(feedURL string, ctx context.Context) (*gofeed.Feed, error)
}

// Service handles RSS feed operations
type Service struct {
	queries    db.Querier
	parser     FeedParser
	logger     *slog.Logger
	httpClient *http.Client
}

// NewService creates a new feed service
func NewService(querier db.Querier, logger *slog.Logger) *Service {
	return &Service{
		queries:    querier,
		parser:     gofeed.NewParser(),
		logger:     logger,
		httpClient: &http.Client{Timeout: faviconHTTPTimeout},
	}
}

// GetQueries returns the database queries instance
func (s *Service) GetQueries() db.Querier {
	return s.queries
}

// ValidateFeed fetches feedURL and confirms it parses as RSS/Atom, without
// storing anything. Used at feed-creation time so a bad URL is rejected
// immediately instead of silently failing on the next refresh.
func (s *Service) ValidateFeed(ctx context.Context, feedURL string) error {
	if _, err := s.parser.ParseURLWithContext(feedURL, ctx); err != nil {
		return fmt.Errorf("not a valid RSS/Atom feed: %w", err)
	}
	return nil
}

// RefreshFeed fetches and updates posts for a specific feed
func (s *Service) RefreshFeed(ctx context.Context, feedID int32) (int, error) {
	logger := ctx.Value("logger").(*slog.Logger)
	logger.Info("refreshing feed", "feed_id", feedID)

	// Get feed details
	feed, err := s.queries.GetFeed(ctx, feedID)
	if err != nil {
		return 0, fmt.Errorf("failed to get feed: %w", err)
	}

	// Parse RSS feed
	parsedFeed, err := s.parser.ParseURLWithContext(feed.Url, ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to parse feed: %w", err)
	}

	// Update feed metadata
	_, err = s.queries.UpdateFeed(ctx, db.UpdateFeedParams{
		ID: feedID,
		Title: func() string {
			if parsedFeed.Title != "" {
				return parsedFeed.Title
			}
			return feed.Title
		}(),
		Description: pgtype.Text(sql.NullString{
			String: parsedFeed.Description,
			Valid:  parsedFeed.Description != "",
		}),
		Link: pgtype.Text(sql.NullString{
			String: parsedFeed.Link,
			Valid:  parsedFeed.Link != "",
		}),
	})
	if err != nil {
		logger.Error("failed to update feed metadata", "error", err)
	}

	// Discover and store the site's favicon (best-effort; a failure here
	// shouldn't fail the whole refresh).
	homePageURL := parsedFeed.Link
	if homePageURL == "" && feed.Link.Valid {
		homePageURL = feed.Link.String
	}
	if homePageURL != "" {
		if faviconURL, err := discoverFaviconURL(ctx, s.httpClient, homePageURL); err != nil {
			logger.Warn("failed to discover favicon", "error", err, "home_page_url", homePageURL)
		} else if _, err := s.queries.UpdateFeedFavicon(ctx, db.UpdateFeedFaviconParams{
			ID:         feedID,
			FaviconUrl: pgtype.Text(sql.NullString{String: faviconURL, Valid: true}),
		}); err != nil {
			logger.Warn("failed to update feed favicon", "error", err)
		}
	}

	// Add/update posts
	postsAdded := 0
	for _, item := range parsedFeed.Items {
		guid := item.GUID
		if guid == "" {
			guid = item.Link
		}

		publishedAt := pgtype.Timestamp{Valid: false}
		if item.PublishedParsed != nil {
			publishedAt = pgtype.Timestamp{
				Time:  *item.PublishedParsed,
				Valid: true,
			}
		}

		_, err := s.queries.CreatePost(ctx, db.CreatePostParams{
			FeedID: feedID,
			Title:  item.Title,
			Description: pgtype.Text(sql.NullString{
				String: item.Description,
				Valid:  item.Description != "",
			}),
			Content: pgtype.Text(sql.NullString{
				String: item.Content,
				Valid:  item.Content != "",
			}),
			Link: item.Link,
			Author: pgtype.Text(sql.NullString{
				String: func() string {
					if item.Author != nil {
						return item.Author.Name
					}
					return ""
				}(),
				Valid: item.Author != nil && item.Author.Name != "",
			}),
			PublishedAt: publishedAt,
			Guid:        guid,
		})

		if err != nil {
			logger.Warn("failed to create post", "error", err, "guid", guid)
		} else {
			postsAdded++
		}
	}

	// Update last fetched timestamp
	err = s.queries.UpdateFeedLastFetched(ctx, feedID)
	if err != nil {
		logger.Error("failed to update last fetched timestamp", "error", err)
	}

	logger.Info("feed refreshed", "feed_id", feedID, "posts_added", postsAdded)
	return postsAdded, nil
}

// RefreshAllFeeds fetches and updates posts for all feeds
func (s *Service) RefreshAllFeeds(ctx context.Context) (int, int, error) {
	logger := ctx.Value("logger").(*slog.Logger)
	logger.Info("refreshing all feeds")

	feeds, err := s.queries.ListFeeds(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to list feeds: %w", err)
	}

	feedsUpdated := 0
	totalPostsAdded := 0

	for _, feed := range feeds {
		postsAdded, err := s.RefreshFeed(ctx, feed.ID)
		if err != nil {
			logger.Error("failed to refresh feed", "feed_id", feed.ID, "error", err)
			continue
		}
		feedsUpdated++
		totalPostsAdded += postsAdded
	}

	logger.Info(
		"all feeds refreshed",
		"feeds_updated",
		feedsUpdated,
		"posts_added",
		totalPostsAdded,
	)
	return feedsUpdated, totalPostsAdded, nil
}
