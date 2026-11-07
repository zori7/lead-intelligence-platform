package llmsettings

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const defaultRow = "default"

// Store persists settings in llm_node_settings. The table always holds
// exactly five rows (seeded by migration 0003); the store only updates them.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func (s *Store) Get(ctx context.Context) (Settings, error) {
	return get(ctx, s.db)
}

func get(ctx context.Context, q querier) (Settings, error) {
	rows, err := q.Query(ctx, `SELECT node, model, temperature::float8, updated_at FROM llm_node_settings`)
	if err != nil {
		return Settings{}, fmt.Errorf("get llm settings: %w", err)
	}
	defer rows.Close()

	out := Settings{Nodes: make(map[Node]Setting, len(Nodes))}
	for rows.Next() {
		var (
			node    string
			setting Setting
			updated time.Time
		)
		if err := rows.Scan(&node, &setting.Model, &setting.Temperature, &updated); err != nil {
			return Settings{}, fmt.Errorf("scan llm settings: %w", err)
		}
		if node == defaultRow {
			out.Default = setting
		} else {
			out.Nodes[Node(node)] = setting
		}
		if updated.After(out.UpdatedAt) {
			out.UpdatedAt = updated
		}
	}
	if err := rows.Err(); err != nil {
		return Settings{}, fmt.Errorf("read llm settings: %w", err)
	}
	return out, nil
}

// Replace overwrites all five rows in one transaction, so a job claimed
// concurrently sees either the old or the new settings, never a mix.
func (s *Store) Replace(ctx context.Context, next Settings) (Settings, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	update := func(node string, st Setting) error {
		tag, err := tx.Exec(ctx,
			`UPDATE llm_node_settings SET model = $2, temperature = $3, updated_at = now() WHERE node = $1`,
			node, st.Model, st.Temperature)
		if err != nil {
			return fmt.Errorf("update llm setting %s: %w", node, err)
		}
		if tag.RowsAffected() != 1 {
			return fmt.Errorf("llm setting row %s is missing", node)
		}
		return nil
	}
	if err := update(defaultRow, next.Default); err != nil {
		return Settings{}, err
	}
	for _, n := range Nodes {
		if err := update(string(n), next.Nodes[n]); err != nil {
			return Settings{}, err
		}
	}
	saved, err := get(ctx, tx)
	if err != nil {
		return Settings{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Settings{}, fmt.Errorf("commit: %w", err)
	}
	return saved, nil
}
