package lead

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"leadintel/backend/internal/api"
	"leadintel/backend/internal/db"
	"leadintel/backend/migrations"
)

// newTestStore connects to TEST_DATABASE_URL, applies migrations and empties
// the leads table. Tests are skipped when the variable is not set.
func newTestStore(t *testing.T) (*Store, *pgxpool.Pool) {
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
	if _, err := pool.Exec(ctx, "TRUNCATE leads CASCADE"); err != nil {
		t.Fatal(err)
	}
	return NewStore(pool), pool
}

func mustCreate(t *testing.T, s *Store, req api.LeadCreate) Lead {
	t.Helper()
	f, err := ValidateCreate(req)
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.Create(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestStoreCRUD(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()

	created := mustCreate(t, s, api.LeadCreate{CompanyName: "Acme", Industry: val("SaaS")})
	if created.Status != api.StatusNew || created.ID == uuid.Nil || created.CreatedAt.IsZero() {
		t.Fatalf("unexpected created lead: %+v", created)
	}

	got, err := s.Get(ctx, created.ID)
	if err != nil || got.CompanyName != "Acme" || got.Industry == nil || *got.Industry != "SaaS" {
		t.Fatalf("get: %+v, %v", got, err)
	}

	changes, err := ValidatePatch(api.LeadPatch{
		Status:   ptrTo(api.StatusQualified),
		Industry: null[string](), // clear
	}, []byte(`{"status":"qualified","industry":null}`))
	if err != nil {
		t.Fatal(err)
	}
	updated, err := s.Update(ctx, created.ID, changes)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != api.StatusQualified || updated.Industry != nil || !updated.UpdatedAt.After(created.UpdatedAt) {
		t.Fatalf("update: %+v", updated)
	}

	if err := s.Delete(ctx, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get after delete: %v", err)
	}
	if err := s.Delete(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second delete: %v", err)
	}
	if _, err := s.Update(ctx, created.ID, changes); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update after delete: %v", err)
	}
}

func TestStoreList(t *testing.T) {
	s, pool := newTestStore(t)
	ctx := context.Background()

	mustCreate(t, s, api.LeadCreate{CompanyName: "Acme Corp", ContactName: val("Ann Lee")})
	mustCreate(t, s, api.LeadCreate{CompanyName: "Globex", Status: ptrTo(api.StatusWon)})
	mustCreate(t, s, api.LeadCreate{CompanyName: "100% Organic"})
	scored := mustCreate(t, s, api.LeadCreate{CompanyName: "Initech"})
	if _, err := pool.Exec(ctx, "UPDATE leads SET ai_score = 90 WHERE id = $1", scored.ID); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name      string
		params    ListParams
		wantTotal int
		wantFirst string
	}{
		{"all, newest first", ListParams{Sort: api.SortCreatedDesc, Limit: 50}, 4, "Initech"},
		{"by status", ListParams{Status: ptrTo(api.StatusWon), Sort: api.SortCreatedDesc, Limit: 50}, 1, "Globex"},
		{"search company", ListParams{Query: "acme", Sort: api.SortCreatedDesc, Limit: 50}, 1, "Acme Corp"},
		{"search contact", ListParams{Query: "ann", Sort: api.SortCreatedDesc, Limit: 50}, 1, "Acme Corp"},
		{"wildcards are literal", ListParams{Query: "%", Sort: api.SortCreatedDesc, Limit: 50}, 1, "100% Organic"},
		{"score sort", ListParams{Sort: api.SortScoreDesc, Limit: 50}, 4, "Initech"},
		{"pagination keeps total", ListParams{Sort: api.SortCreatedDesc, Limit: 1, Offset: 1}, 4, "100% Organic"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			items, total, err := s.List(ctx, tt.params)
			if err != nil {
				t.Fatal(err)
			}
			if total != tt.wantTotal || len(items) == 0 || items[0].CompanyName != tt.wantFirst {
				t.Fatalf("total=%d first=%v, want total=%d first=%q", total, items, tt.wantTotal, tt.wantFirst)
			}
		})
	}
}

func TestDBRejectsInvalidStatus(t *testing.T) {
	_, pool := newTestStore(t)
	_, err := pool.Exec(context.Background(), "INSERT INTO leads (company_name, status) VALUES ('X', 'archived')")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Fatalf("expected check_violation, got %v", err)
	}
}
