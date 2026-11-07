package llmsettings

import (
	"context"
	"os"
	"reflect"
	"testing"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/db"
	"leadintel/backend/migrations"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping store integration tests")
	}
	ctx := context.Background()
	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	sqlDB := stdlib.OpenDBFromPool(pool)
	t.Cleanup(func() { _ = sqlDB.Close() })
	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "UPDATE llm_node_settings SET model = NULL, temperature = NULL"); err != nil {
		t.Fatal(err)
	}
	return NewStore(pool)
}

func TestStoreRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	empty, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if empty.Configured() || len(empty.Nodes) != len(Nodes) {
		t.Fatalf("seeded settings: %+v", empty)
	}

	next := Settings{
		Default: Setting{Model: ptr("base"), Temperature: ptr(0.25)},
		Nodes: map[Node]Setting{
			api.NodeExtractCompany: {Model: ptr("big")},
			api.NodeAnalyzeIntent:  {Temperature: ptr(1.5)},
		},
	}
	saved, err := s.Replace(ctx, next)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, saved) || saved.UpdatedAt.IsZero() {
		t.Fatalf("Get after Replace differs:\n%+v\n%+v", got, saved)
	}
	want := Resolved{
		api.NodeExtractCompany:          {"big", 0.25},
		api.NodeAnalyzeFit:              {"base", 0.25},
		api.NodeAnalyzeIntent:           {"base", 1.5},
		api.NodeGenerateRecommendations: {"base", 0.25},
	}
	if r, _ := Resolve(got); !reflect.DeepEqual(r, want) {
		t.Fatalf("resolved %v, want %v", r, want)
	}
}
