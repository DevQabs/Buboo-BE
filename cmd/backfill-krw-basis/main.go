// backfill-krw-basis 는 원화 취득가(avg_krw_price)가 없거나 손상된 USD 종목을
// 그 종목의 매수 기록 환율로 다시 채운다. 기본은 목록만 출력하고 되돌린다(dry-run).
// -apply 를 줘야 쓴다. 조회부터 쓰기까지 한 DB 트랜잭션이다.
//
//	DATABASE_URL=... go run ./cmd/backfill-krw-basis          # 목록만
//	DATABASE_URL=... go run ./cmd/backfill-krw-basis -apply   # 목록 검토 후 쓰기
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"text/tabwriter"

	"github.com/jackc/pgx/v5"
	"github.com/yourname/couple-app/internal/db"
	"github.com/yourname/couple-app/internal/service"
)

type buyLeg struct{ Qty, Price, FX float64 }

type proposal struct {
	Value  float64
	Source string
	OK     bool
}

// propose 는 달러 평단 × (환율이 기록된 매수의 달러 금액 가중 환율)을 제안한다.
// 환율 기록이 하나도 없거나 결과가 범위 밖이면 추측하지 않고 수동 대상으로 남긴다.
func propose(avgUSD float64, buys []buyLeg) proposal {
	var usd, krw, fxQty, allQty float64
	n := 0
	for _, b := range buys {
		allQty += b.Qty
		if b.FX <= 0 {
			continue
		}
		usd += b.Qty * b.Price
		krw += b.Qty * b.Price * b.FX
		fxQty += b.Qty
		n++
	}
	if n == 0 || usd <= 0 {
		return proposal{Source: "수동 환율 필요: 환율이 기록된 매수 없음"}
	}
	fx := krw / usd
	value := avgUSD * fx
	if err := service.ValidateKRWCostBasis(avgUSD, value); err != nil {
		return proposal{Value: value, Source: "수동 환율 필요: " + err.Error()}
	}
	return proposal{
		Value:  value,
		Source: fmt.Sprintf("매수 %d건 가중환율 %.2f (환율 있는 매수 %.4f주 / 전체 매수 %.4f주)", n, fx, fxQty, allQty),
		OK:     true,
	}
}

type target struct {
	ID, CoupleID, Symbol string
	Qty, AvgUSD, AvgKRW  float64
}

func loadTargets(ctx context.Context, tx pgx.Tx) ([]target, error) {
	rows, err := tx.Query(ctx,
		`SELECT id, couple_id, symbol, quantity, average_price, avg_krw_price
		 FROM stock_assets WHERE currency = 'USD' ORDER BY couple_id, symbol FOR UPDATE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []target
	for rows.Next() {
		var t target
		if err := rows.Scan(&t.ID, &t.CoupleID, &t.Symbol, &t.Qty, &t.AvgUSD, &t.AvgKRW); err != nil {
			return nil, err
		}
		if service.ValidateKRWCostBasis(t.AvgUSD, t.AvgKRW) != nil {
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

func loadBuys(ctx context.Context, tx pgx.Tx, assetID string) ([]buyLeg, error) {
	rows, err := tx.Query(ctx,
		`SELECT quantity, price, exchange_rate_at_tx FROM stock_transactions
		 WHERE stock_asset_id = $1 AND type = 'buy'`, assetID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []buyLeg
	for rows.Next() {
		var b buyLeg
		if err := rows.Scan(&b.Qty, &b.Price, &b.FX); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func main() {
	apply := flag.Bool("apply", false, "제안값을 DB 에 쓴다. 없으면 목록만 출력하고 되돌린다")
	flag.Parse()
	ctx := context.Background()

	pool, err := db.New(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()

	tx, err := pool.Begin(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback(ctx) // Commit 뒤에는 아무 일도 하지 않는다

	targets, err := loadTargets(ctx, tx)
	if err != nil {
		log.Fatal(err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "상태\tid\tcouple_id\tsymbol\t수량\t달러평단\t현재 원화평단\t제안 원화평단\t근거")
	auto, manual := 0, 0
	for _, t := range targets {
		buys, err := loadBuys(ctx, tx, t.ID)
		if err != nil {
			log.Fatal(err)
		}
		p := propose(t.AvgUSD, buys)
		status := "자동"
		if p.OK {
			auto++
		} else {
			status = "수동"
			manual++
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%.4f\t%.4f\t%.2f\t%.2f\t%s\n",
			status, t.ID, t.CoupleID, t.Symbol, t.Qty, t.AvgUSD, t.AvgKRW, p.Value, p.Source)
		if *apply && p.OK {
			if _, err := tx.Exec(ctx,
				`UPDATE stock_assets SET avg_krw_price = $2, updated_at = NOW() WHERE id = $1`,
				t.ID, p.Value); err != nil {
				log.Fatal(err)
			}
		}
	}
	w.Flush()
	fmt.Printf("\n대상 %d건: 자동 %d건, 수동 환율 필요 %d건\n", len(targets), auto, manual)

	if !*apply {
		fmt.Println("dry-run: 아무것도 쓰지 않았습니다. 목록을 검토한 뒤 -apply 로 다시 실행하세요.")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("적용 완료: %d건. 수동 %d건은 화면의 '주식 수정 > 원화 평균 매입가'로 입력하세요.\n", auto, manual)
}
