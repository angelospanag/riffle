package post_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/angelospanag/riffle/internal/db"
	apimiddleware "github.com/angelospanag/riffle/internal/middleware"
	"github.com/angelospanag/riffle/internal/post"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

type mockQuerier struct {
	listPostsResult []db.Post
	listPostsErr    error
	getPostResult   db.Post
	getPostErr      error
}

func (m *mockQuerier) CountUnreadPosts(_ context.Context) (int64, error) { return 0, nil }
func (m *mockQuerier) CountUnreadPostsByFeed(_ context.Context, _ int32) (int64, error) {
	return 0, nil
}

func (m *mockQuerier) CreateFeed(_ context.Context, _ db.CreateFeedParams) (db.Feed, error) {
	return db.Feed{}, nil
}

func (m *mockQuerier) CreatePost(_ context.Context, _ db.CreatePostParams) (db.Post, error) {
	return db.Post{}, nil
}
func (m *mockQuerier) DeleteFeed(_ context.Context, _ int32) error { return nil }
func (m *mockQuerier) DeletePost(_ context.Context, _ int32) error { return nil }
func (m *mockQuerier) GetFeed(_ context.Context, _ int32) (db.Feed, error) {
	return db.Feed{}, nil
}

func (m *mockQuerier) GetFeedByURL(_ context.Context, _ string) (db.Feed, error) {
	return db.Feed{}, nil
}
func (m *mockQuerier) ListFeeds(_ context.Context) ([]db.Feed, error)      { return nil, nil }
func (m *mockQuerier) MarkAllPostsAsRead(_ context.Context, _ int32) error { return nil }
func (m *mockQuerier) MarkPostAsRead(_ context.Context, _ int32) error     { return nil }
func (m *mockQuerier) MarkPostAsUnread(_ context.Context, _ int32) error   { return nil }
func (m *mockQuerier) UpdateFeed(_ context.Context, _ db.UpdateFeedParams) (db.Feed, error) {
	return db.Feed{}, nil
}

func (m *mockQuerier) UpdateFeedFavicon(
	_ context.Context,
	_ db.UpdateFeedFaviconParams,
) (db.Feed, error) {
	return db.Feed{}, nil
}
func (m *mockQuerier) UpdateFeedLastFetched(_ context.Context, _ int32) error { return nil }

func (m *mockQuerier) ListPostsByFeed(
	_ context.Context,
	_ db.ListPostsByFeedParams,
) ([]db.Post, error) {
	return nil, nil
}

func (m *mockQuerier) ListUnreadPosts(
	_ context.Context,
	_ db.ListUnreadPostsParams,
) ([]db.Post, error) {
	return nil, nil
}

func (m *mockQuerier) ListUnreadPostsByFeed(
	_ context.Context,
	_ db.ListUnreadPostsByFeedParams,
) ([]db.Post, error) {
	return nil, nil
}

func (m *mockQuerier) GetPost(_ context.Context, _ int32) (db.Post, error) {
	return m.getPostResult, m.getPostErr
}

func (m *mockQuerier) ListPosts(_ context.Context, _ db.ListPostsParams) ([]db.Post, error) {
	return m.listPostsResult, m.listPostsErr
}

func newTestRouter(q db.Querier) *chi.Mux {
	router := chi.NewMux()
	api := humachi.New(router, huma.DefaultConfig("test", "1.0.0"))
	api.UseMiddleware(apimiddleware.Logging(slog.Default()))
	post.RegisterRoutes(api, q, slog.Default())
	return router
}

func TestListPosts_Empty(t *testing.T) {
	q := &mockQuerier{listPostsResult: []db.Post{}}
	router := newTestRouter(q)

	req := httptest.NewRequest(http.MethodGet, "/posts", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGetPost_NotFound(t *testing.T) {
	q := &mockQuerier{getPostErr: pgx.ErrNoRows}
	router := newTestRouter(q)

	req := httptest.NewRequest(http.MethodGet, "/posts/999", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", w.Code, w.Body.String())
	}
}
