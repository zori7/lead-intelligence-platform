package lead

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"leadintel/backend/internal/api"
)

const columns = `id, company_name, website, contact_name, contact_email, contact_title,
	industry, company_size, notes, status, ai_score, ai_tier, last_analyzed_at, created_at, updated_at`

// Store persists leads in PostgreSQL using hand-written, parameterized SQL.
type Store struct {
	db *pgxpool.Pool
}

func NewStore(db *pgxpool.Pool) *Store { return &Store{db: db} }

func (s *Store) List(ctx context.Context, p ListParams) ([]Lead, int, error) {
	var (
		where []string
		args  []any
	)
	arg := func(v any) string {
		args = append(args, v)
		return "$" + strconv.Itoa(len(args))
	}
	if p.Status != nil {
		where = append(where, "status = "+arg(*p.Status))
	}
	if p.Query != "" {
		pattern := arg("%" + escapeLike(p.Query) + "%")
		where = append(where, fmt.Sprintf(`(company_name ILIKE %[1]s OR contact_name ILIKE %[1]s)`, pattern))
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRow(ctx, "SELECT count(*) FROM leads"+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count leads: %w", err)
	}

	// Sort is an allowlisted enum; it is never interpolated from user input.
	orderBy := "created_at DESC, id DESC"
	if p.Sort == api.SortScoreDesc {
		orderBy = "ai_score DESC NULLS LAST, created_at DESC, id DESC"
	}
	query := "SELECT " + columns + " FROM leads" + whereSQL +
		" ORDER BY " + orderBy + " LIMIT " + arg(p.Limit) + " OFFSET " + arg(p.Offset)

	rows, err := s.db.Query(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list leads: %w", err)
	}
	leads, err := pgx.CollectRows(rows, pgx.RowToStructByName[Lead])
	if err != nil {
		return nil, 0, fmt.Errorf("scan leads: %w", err)
	}
	return leads, total, nil
}

// Stats counts all leads in one pass. A lead counts as analyzed once a
// successful analysis has written its score and tier.
func (s *Store) Stats(ctx context.Context) (api.LeadStats, error) {
	var st api.LeadStats
	err := s.db.QueryRow(ctx, `
		SELECT count(*),
		       count(*) FILTER (WHERE ai_score IS NOT NULL),
		       count(*) FILTER (WHERE ai_tier = 'hot'),
		       count(*) FILTER (WHERE ai_tier = 'warm'),
		       count(*) FILTER (WHERE ai_tier = 'cold')
		FROM leads`).Scan(&st.Total, &st.Analyzed, &st.ByTier.Hot, &st.ByTier.Warm, &st.ByTier.Cold)
	if err != nil {
		return api.LeadStats{}, fmt.Errorf("lead stats: %w", err)
	}
	return st, nil
}

func (s *Store) Get(ctx context.Context, id uuid.UUID) (Lead, error) {
	rows, _ := s.db.Query(ctx, "SELECT "+columns+" FROM leads WHERE id = $1", id)
	return collectOne(rows, "get lead")
}

func (s *Store) Create(ctx context.Context, f Fields) (Lead, error) {
	rows, _ := s.db.Query(ctx, `
		INSERT INTO leads (company_name, website, contact_name, contact_email, contact_title,
		                   industry, company_size, notes, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING `+columns,
		f.CompanyName, f.Website, f.ContactName, f.ContactEmail, f.ContactTitle,
		f.Industry, f.CompanySize, f.Notes, f.Status)
	return collectOne(rows, "create lead")
}

// Update applies validated changes atomically. Column names come from
// ValidatePatch (a fixed set), never from user input.
func (s *Store) Update(ctx context.Context, id uuid.UUID, changes []Change) (Lead, error) {
	if len(changes) == 0 {
		return s.Get(ctx, id)
	}
	sets := make([]string, 0, len(changes)+1)
	args := make([]any, 0, len(changes)+1)
	for _, c := range changes {
		args = append(args, c.Value)
		sets = append(sets, c.Column+" = $"+strconv.Itoa(len(args)))
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)

	rows, _ := s.db.Query(ctx,
		"UPDATE leads SET "+strings.Join(sets, ", ")+" WHERE id = $"+strconv.Itoa(len(args))+" RETURNING "+columns,
		args...)
	return collectOne(rows, "update lead")
}

func (s *Store) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := s.db.Exec(ctx, "DELETE FROM leads WHERE id = $1", id)
	if err != nil {
		return fmt.Errorf("delete lead: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func collectOne(rows pgx.Rows, op string) (Lead, error) {
	l, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[Lead])
	if errors.Is(err, pgx.ErrNoRows) {
		return Lead{}, ErrNotFound
	}
	if err != nil {
		return Lead{}, fmt.Errorf("%s: %w", op, err)
	}
	return l, nil
}

// escapeLike escapes LIKE wildcards so user input matches literally
// (PostgreSQL's default LIKE escape character is the backslash).
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
