package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/yourname/couple-app/internal/models"
)

type PgRoadmapRepository struct {
	db *pgxpool.Pool
}

func NewPgRoadmapRepository(db *pgxpool.Pool) *PgRoadmapRepository {
	return &PgRoadmapRepository{db: db}
}

// ActiveGoal은 부부의 현재 목표다. 없으면 (nil, nil)이다.
func (r *PgRoadmapRepository) ActiveGoal(ctx context.Context, coupleID string) (*models.RoadmapGoal, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, couple_id, title, target_krw, target_date, birth_year, is_active, created_at, updated_at
		   FROM roadmap_goals WHERE couple_id = $1 AND is_active = TRUE
		  ORDER BY created_at DESC LIMIT 1`, coupleID)
	var g models.RoadmapGoal
	err := row.Scan(&g.ID, &g.CoupleID, &g.Title, &g.TargetKRW, &g.TargetDate,
		&g.BirthYear, &g.IsActive, &g.CreatedAt, &g.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &g, nil
}

// UpsertGoal은 목표를 저장한다. 목표는 부부당 하나만 활성으로 둔다.
func (r *PgRoadmapRepository) UpsertGoal(ctx context.Context, g *models.RoadmapGoal) (*models.RoadmapGoal, error) {
	g.UpdatedAt = time.Now().UTC()
	_, err := r.db.Exec(ctx,
		`INSERT INTO roadmap_goals (id, couple_id, title, target_krw, target_date, birth_year, is_active, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, TRUE, $7)
		 ON CONFLICT (id) DO UPDATE
		 SET title = EXCLUDED.title, target_krw = EXCLUDED.target_krw,
		     target_date = EXCLUDED.target_date, birth_year = EXCLUDED.birth_year,
		     updated_at = EXCLUDED.updated_at`,
		g.ID, g.CoupleID, g.Title, g.TargetKRW, g.TargetDate, g.BirthYear, g.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return g, nil
}

// Assumptions는 목표에 딸린 가정이다. 없으면 (nil, nil)이다.
func (r *PgRoadmapRepository) Assumptions(ctx context.Context, goalID string) (*models.RoadmapAssumptions, error) {
	row := r.db.QueryRow(ctx,
		`SELECT id, couple_id, goal_id, price_growth, dividend_tax_rate, other_assets_krw,
		        contributions, dividend_plan, dividend_symbols, updated_at
		   FROM roadmap_assumptions WHERE goal_id = $1`, goalID)
	var (
		a                          models.RoadmapAssumptions
		contribs, divPlan, divSyms []byte
	)
	err := row.Scan(&a.ID, &a.CoupleID, &a.GoalID, &a.PriceGrowth, &a.DividendTaxRate,
		&a.OtherAssetsKRW, &contribs, &divPlan, &divSyms, &a.UpdatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(contribs, &a.Contributions); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(divPlan, &a.DividendPlan); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(divSyms, &a.DividendSymbols); err != nil {
		return nil, err
	}
	return &a, nil
}

func (r *PgRoadmapRepository) UpsertAssumptions(ctx context.Context, a *models.RoadmapAssumptions) (*models.RoadmapAssumptions, error) {
	contribs, err := json.Marshal(a.Contributions)
	if err != nil {
		return nil, err
	}
	divPlan, err := json.Marshal(a.DividendPlan)
	if err != nil {
		return nil, err
	}
	if a.DividendSymbols == nil {
		a.DividendSymbols = []string{}
	}
	divSyms, err := json.Marshal(a.DividendSymbols)
	if err != nil {
		return nil, err
	}
	a.UpdatedAt = time.Now().UTC()
	_, err = r.db.Exec(ctx,
		`INSERT INTO roadmap_assumptions
		   (id, couple_id, goal_id, price_growth, dividend_tax_rate, other_assets_krw,
		    contributions, dividend_plan, dividend_symbols, updated_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		 ON CONFLICT (goal_id) DO UPDATE
		 SET price_growth = EXCLUDED.price_growth,
		     dividend_tax_rate = EXCLUDED.dividend_tax_rate,
		     other_assets_krw = EXCLUDED.other_assets_krw,
		     contributions = EXCLUDED.contributions,
		     dividend_plan = EXCLUDED.dividend_plan,
		     dividend_symbols = EXCLUDED.dividend_symbols,
		     updated_at = EXCLUDED.updated_at`,
		a.ID, a.CoupleID, a.GoalID, a.PriceGrowth, a.DividendTaxRate, a.OtherAssetsKRW,
		string(contribs), string(divPlan), string(divSyms), a.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return a, nil
}

// UpsertSnapshot은 해당 월의 순자산 실적을 적재한다. 같은 달은 덮어쓴다.
func (r *PgRoadmapRepository) UpsertSnapshot(ctx context.Context, s *models.NetWorthSnapshot) (*models.NetWorthSnapshot, error) {
	_, err := r.db.Exec(ctx,
		`INSERT INTO networth_snapshots
		   (id, couple_id, snapshot_month, stock_krw, asset_krw, liability_krw, net_worth_krw)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (couple_id, snapshot_month) DO UPDATE
		 SET stock_krw = EXCLUDED.stock_krw, asset_krw = EXCLUDED.asset_krw,
		     liability_krw = EXCLUDED.liability_krw, net_worth_krw = EXCLUDED.net_worth_krw`,
		s.ID, s.CoupleID, s.SnapshotMonth, s.StockKRW, s.AssetKRW, s.LiabilityKRW, s.NetWorthKRW)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// ListSnapshots는 오래된 순으로 전부 준다. 부부 2인용이라 페이징은 두지 않는다.
func (r *PgRoadmapRepository) ListSnapshots(ctx context.Context, coupleID string) ([]models.NetWorthSnapshot, error) {
	rows, err := r.db.Query(ctx,
		`SELECT id, couple_id, snapshot_month, stock_krw, asset_krw, liability_krw, net_worth_krw, created_at
		   FROM networth_snapshots WHERE couple_id = $1 ORDER BY snapshot_month`, coupleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]models.NetWorthSnapshot, 0)
	for rows.Next() {
		var s models.NetWorthSnapshot
		if err := rows.Scan(&s.ID, &s.CoupleID, &s.SnapshotMonth, &s.StockKRW,
			&s.AssetKRW, &s.LiabilityKRW, &s.NetWorthKRW, &s.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// MonthlyContributions는 월별 적립 실적이다.
//
// 적립은 저축과 주식 매수를 합친 돈이다. 주식은 매수에서 매도를 뺀 순매수로
// 본다 — 팔았다 다시 사는 건 새로 넣은 돈이 아니기 때문이다. 원화 환산은
// 거래 시점 환율을 쓴다.
//
// from 이전은 세지 않는다. 앱을 쓰기 전부터 갖고 있던 주식을 등록한 기록이
// 매수로 남아 있어, 그 달 적립이 실제보다 몇 배로 잡힌다.
func (r *PgRoadmapRepository) MonthlyContributions(ctx context.Context, coupleID string, from time.Time) (map[string]int64, error) {
	rows, err := r.db.Query(ctx,
		`SELECT month, SUM(krw)::bigint FROM (
		   SELECT to_char(executed_at, 'YYYY-MM') AS month,
		          SUM(CASE WHEN type = 'buy' THEN 1 ELSE -1 END
		              * quantity * price
		              * CASE WHEN currency = 'USD' AND exchange_rate_at_tx > 0
		                     THEN exchange_rate_at_tx ELSE 1 END) AS krw
		     FROM stock_transactions
		    WHERE couple_id = $1 AND executed_at >= $2
		    GROUP BY 1
		   UNION ALL
		   SELECT to_char(date, 'YYYY-MM') AS month, SUM(amount) AS krw
		     FROM transactions
		    WHERE couple_id = $1 AND date >= $2 AND category LIKE '저축%'
		    GROUP BY 1
		 ) t GROUP BY month`, coupleID, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make(map[string]int64)
	for rows.Next() {
		var month string
		var krw int64
		if err := rows.Scan(&month, &krw); err != nil {
			return nil, err
		}
		out[month] = krw
	}
	return out, rows.Err()
}

// Baseline은 목표에 확정된 계획선이다. 없으면 (nil, nil)이다.
func (r *PgRoadmapRepository) Baseline(ctx context.Context, goalID string) (*models.RoadmapBaseline, error) {
	row := r.db.QueryRow(ctx,
		`SELECT goal_id, couple_id, anchor_month, anchor_net_worth_krw, price_growth,
		        dividend_yield, years, months, created_at
		   FROM roadmap_baselines WHERE goal_id = $1`, goalID)
	var (
		b             models.RoadmapBaseline
		years, months []byte
	)
	err := row.Scan(&b.GoalID, &b.CoupleID, &b.AnchorMonth, &b.AnchorNetWorthKRW,
		&b.PriceGrowth, &b.DividendYield, &years, &months, &b.CreatedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(years, &b.Years); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(months, &b.Months); err != nil {
		return nil, err
	}
	return &b, nil
}

// SaveBaseline은 계획선을 저장한다. 이미 있으면 그대로 두어, 동시에 두 요청이
// 만들어도 먼저 저장된 선이 유지된다.
func (r *PgRoadmapRepository) SaveBaseline(ctx context.Context, b *models.RoadmapBaseline) error {
	years, err := json.Marshal(b.Years)
	if err != nil {
		return err
	}
	months, err := json.Marshal(b.Months)
	if err != nil {
		return err
	}
	_, err = r.db.Exec(ctx,
		`INSERT INTO roadmap_baselines
		   (goal_id, couple_id, anchor_month, anchor_net_worth_krw, price_growth,
		    dividend_yield, years, months)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (goal_id) DO NOTHING`,
		b.GoalID, b.CoupleID, b.AnchorMonth, b.AnchorNetWorthKRW, b.PriceGrowth,
		b.DividendYield, string(years), string(months))
	return err
}

// DeleteBaseline은 계획선을 지운다. 다음 조회에서 새로 만들어진다.
func (r *PgRoadmapRepository) DeleteBaseline(ctx context.Context, goalID string) error {
	_, err := r.db.Exec(ctx, `DELETE FROM roadmap_baselines WHERE goal_id = $1`, goalID)
	return err
}
