package repository

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

var defaultExpenseCategories = []string{"식비", "카페", "장보기", "주거", "교통", "의료", "문화", "쇼핑", "기타"}
var defaultIncomeCategories  = []string{"급여", "부수입", "용돈", "배당", "기타"}

type PgCategoryRepository struct {
	db *pgxpool.Pool
}

func NewPgCategoryRepository(db *pgxpool.Pool) *PgCategoryRepository {
	return &PgCategoryRepository{db: db}
}

func (r *PgCategoryRepository) Get(ctx context.Context, coupleID string) (*models.Categories, error) {
	row := r.db.QueryRow(ctx,
		`SELECT expense_categories, income_categories FROM categories WHERE couple_id = $1`,
		coupleID)
	cats := &models.Categories{}
	if err := row.Scan(&cats.Expense, &cats.Income); err != nil {
		return &models.Categories{Expense: defaultExpenseCategories, Income: defaultIncomeCategories}, nil
	}
	return cats, nil
}

func (r *PgCategoryRepository) Update(ctx context.Context, coupleID string, cats *models.Categories) (*models.Categories, error) {
	_, err := r.db.Exec(ctx,
		`INSERT INTO categories (couple_id, expense_categories, income_categories)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (couple_id) DO UPDATE
		 SET expense_categories = EXCLUDED.expense_categories,
		     income_categories  = EXCLUDED.income_categories`,
		coupleID, cats.Expense, cats.Income)
	if err != nil {
		return nil, err
	}
	return cats, nil
}
