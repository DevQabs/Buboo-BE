package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ownedTables is the whitelist of tables the ownership check may query;
// the table name is concatenated into SQL, so it must never come from input.
var ownedTables = map[string]bool{
	"transactions":   true,
	"stock_assets":   true,
	"other_assets":   true,
	"dividends":      true,
	"fixed_expenses": true,
	"schedules":      true,
	"diaries":        true,
	"baby_moments":   true,
	"baby_media":     true,
	"baby_growth":    true,
}

type PgOwnership struct{ db *pgxpool.Pool }

func NewPgOwnership(db *pgxpool.Pool) *PgOwnership { return &PgOwnership{db: db} }

func (o *PgOwnership) Owns(ctx context.Context, table, id, coupleID string) (bool, error) {
	if !ownedTables[table] {
		return false, fmt.Errorf("ownership: unknown table %q", table)
	}
	var ok bool
	err := o.db.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM `+table+` WHERE id = $1 AND couple_id = $2)`,
		id, coupleID).Scan(&ok)
	return ok, err
}
