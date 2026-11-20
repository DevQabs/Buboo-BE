package service

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/yourname/couple-app/internal/models"
)

var replayDay0 = time.Date(2026, 1, 1, 10, 0, 0, 0, models.KST)

// leg 은 i 번째 날의 기록이다.
func leg(i int, typ models.StockTxType, qty, price, fx float64) models.StockTransaction {
	return models.StockTransaction{ID: string(rune('a' + i)), Type: typ, Quantity: qty, Price: price, ExchangeRateAtTx: fx,
		ExecutedAt: replayDay0.AddDate(0, 0, i)}
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

func TestReplay(t *testing.T) {
	B, S := models.StockTxBuy, models.StockTxSell
	cases := []struct {
		name    string
		usd     bool
		open    Position
		txs     []models.StockTransaction
		want    Position
		wantPnL []float64 // 매도 기록 순서대로
		wantErr error
	}{
		{name: "매수만", txs: []models.StockTransaction{leg(0, B, 10, 100, 0), leg(1, B, 10, 200, 0)},
			want: Position{20, 150, 150}},
		{name: "일부 매도는 평단을 두고 손익만", txs: []models.StockTransaction{leg(0, B, 10, 100, 0), leg(1, B, 10, 200, 0), leg(2, S, 5, 180, 0)},
			want: Position{15, 150, 150}, wantPnL: []float64{150}},
		{name: "다 팔고 다시 사면 평단이 새로 시작", txs: []models.StockTransaction{leg(0, B, 10, 100, 0), leg(1, S, 10, 120, 0), leg(2, B, 5, 200, 0)},
			want: Position{5, 200, 200}, wantPnL: []float64{200}},
		{name: "가운데 매수를 지우면 뒤 매도 손익이 바뀐다", txs: []models.StockTransaction{leg(0, B, 10, 100, 0), leg(2, S, 5, 180, 0)},
			want: Position{5, 100, 100}, wantPnL: []float64{400}},
		{name: "USD 매수 환율 1300", usd: true, txs: []models.StockTransaction{leg(0, B, 10, 10, 1300), leg(1, S, 5, 12, 1400)},
			want: Position{5, 10, 13_000}, wantPnL: []float64{(12*1400 - 13_000) * 5}},
		{name: "USD 매수 환율을 1200 으로 고치면 원화 평단·손익이 바뀐다", usd: true, txs: []models.StockTransaction{leg(0, B, 10, 10, 1200), leg(1, S, 5, 12, 1400)},
			want: Position{5, 10, 12_000}, wantPnL: []float64{(12*1400 - 12_000) * 5}},
		{name: "기록 이전 보유분 위에 쌓는다", open: Position{20, 150, 150}, txs: []models.StockTransaction{leg(0, S, 5, 180, 0)},
			want: Position{15, 150, 150}, wantPnL: []float64{150}},
		{name: "보유보다 많이 팔게 되면 거부", txs: []models.StockTransaction{leg(0, B, 10, 100, 0), leg(1, S, 15, 120, 0)},
			wantErr: ErrLedgerNegative},
		{name: "매도를 매수보다 앞 날짜로 옮기면 거부", txs: []models.StockTransaction{leg(0, S, 5, 120, 0), leg(1, B, 10, 100, 0)},
			wantErr: ErrLedgerNegative},
		{name: "환율 없는 USD 매도는 계산하지 않는다", usd: true, txs: []models.StockTransaction{leg(0, B, 10, 10, 1300), leg(1, S, 5, 12, 0)},
			wantErr: ErrLedgerFX},
	}
	for _, c := range cases {
		got, out, err := Replay(c.usd, c.open, c.txs)
		if c.wantErr != nil {
			if !errors.Is(err, c.wantErr) {
				t.Errorf("%s: %v 이어야 한다: %v", c.name, c.wantErr, err)
			}
			continue
		}
		if err != nil || !near(got.Qty, c.want.Qty) || !near(got.Avg, c.want.Avg) || !near(got.AvgKRW, c.want.AvgKRW) {
			t.Errorf("%s: want %+v, got %+v err=%v", c.name, c.want, got, err)
			continue
		}
		var pnl []float64
		for _, o := range out {
			if o.Type == models.StockTxSell {
				pnl = append(pnl, o.RealizedPnL)
			}
		}
		if len(pnl) != len(c.wantPnL) {
			t.Errorf("%s: 매도 손익 %v, want %v", c.name, pnl, c.wantPnL)
			continue
		}
		for i := range pnl {
			if !near(pnl[i], c.wantPnL[i]) {
				t.Errorf("%s: 매도 손익 %v, want %v", c.name, pnl, c.wantPnL)
			}
		}
	}
}

func TestOpening(t *testing.T) {
	B, S := models.StockTxBuy, models.StockTxSell
	// 기록 없이 등록한 20주@150 에 5주@180 매도 기록만 있다 → 지금 15주@150
	sell := leg(1, S, 5, 180, 0)
	sell.AvgPriceAtTx, sell.RealizedPnL = 150, 150
	if got, err := Opening(false, Position{15, 150, 150}, []models.StockTransaction{sell}); err != nil || !near(got.Qty, 20) || !near(got.Avg, 150) {
		t.Errorf("기록 이전 보유분은 20주@150: %+v %v", got, err)
	}
	// 10주@100 (기록 없음) + 매수 기록 10주@200 → 지금 20주@150 → 기록 이전 10주@100
	if got, err := Opening(false, Position{20, 150, 150}, []models.StockTransaction{leg(0, B, 10, 200, 0)}); err != nil || !near(got.Qty, 10) || !near(got.Avg, 100) {
		t.Errorf("기록 이전 보유분은 10주@100: %+v %v", got, err)
	}
	// 기록이 다 있으면 기록 이전 보유분은 0
	if got, err := Opening(false, Position{20, 150, 150}, []models.StockTransaction{leg(0, B, 10, 100, 0), leg(1, B, 10, 200, 0)}); err != nil || got != (Position{}) {
		t.Errorf("기록이 다 있으면 0: %+v %v", got, err)
	}
	// 다 판 USD 종목: 0주에서 매도를 되돌리면 그 매도의 평단·손익으로 원화 평단을 되살린다
	usdSell := leg(1, S, 10, 12, 1400)
	usdSell.AvgPriceAtTx, usdSell.RealizedPnL = 10, (12*1400-13_000)*10
	if got, err := Opening(true, Position{}, []models.StockTransaction{usdSell}); err != nil || !near(got.Qty, 10) || !near(got.Avg, 10) || !near(got.AvgKRW, 13_000) {
		t.Errorf("다 판 종목 기록 이전 보유분은 10주@$10, 원화 13,000: %+v %v", got, err)
	}
	// 기록보다 지금 수량이 적으면(기록 밖에서 수량을 줄임) 계산하지 않는다
	if _, err := Opening(false, Position{3, 100, 100}, []models.StockTransaction{leg(0, B, 5, 100, 0)}); !errors.Is(err, ErrLedgerMismatch) {
		t.Errorf("ErrLedgerMismatch 여야 한다: %v", err)
	}
}

// 고치지 않고 Opening → Replay 하면 지금 상태와 기록이 그대로 나와야 한다(기존 데이터를 망가뜨리지 않는다).
func TestOpeningThenReplay_고치지않으면그대로다(t *testing.T) {
	B, S := models.StockTxBuy, models.StockTxSell
	txs := []models.StockTransaction{leg(0, B, 3, 11, 1350), leg(1, B, 2, 14, 1420), leg(2, S, 4, 15, 1390), leg(3, B, 6, 9.5, 1460)}
	// 앞선 보유분 7주@$12, 원화 16,000 위에 쌓은 지금 상태를 만든다
	cur, recorded, err := Replay(true, Position{7, 12, 16_000}, txs)
	if err != nil {
		t.Fatal(err)
	}
	open, err := Opening(true, cur, recorded)
	if err != nil || !near(open.Qty, 7) || !near(open.Avg, 12) || !near(open.AvgKRW, 16_000) {
		t.Fatalf("기록 이전 보유분은 7주@$12, 16,000: %+v %v", open, err)
	}
	again, out, err := Replay(true, open, recorded)
	if err != nil || !near(again.Qty, cur.Qty) || !near(again.Avg, cur.Avg) || !near(again.AvgKRW, cur.AvgKRW) {
		t.Fatalf("다시 쌓으면 %+v 여야 한다: %+v %v", cur, again, err)
	}
	for i := range out {
		if !near(out[i].RealizedPnL, recorded[i].RealizedPnL) || !near(out[i].AvgPriceAtTx, recorded[i].AvgPriceAtTx) {
			t.Fatalf("%d번 기록이 바뀌었다: %+v → %+v", i, recorded[i], out[i])
		}
	}

}

func TestCheckLedger_반영순서와거래일순서가다르면_맞지않는다(t *testing.T) {
	// 기록 없이 10주@100 → 13:00 5주@150 매도(손익 250) → 나중에 저장된 저축 5주@200 이 12:00 으로 찍힘. 지금 10주@150.
	sell := models.StockTransaction{ID: "s", Type: models.StockTxSell, Quantity: 5, Price: 150, AvgPriceAtTx: 100, RealizedPnL: 250,
		ExecutedAt: replayDay0.Add(3 * time.Hour), CreatedAt: replayDay0.Add(3 * time.Hour)}
	buy := models.StockTransaction{ID: "b", Type: models.StockTxBuy, Quantity: 5, Price: 200, Source: models.StockTxSourceSaving,
		ExecutedAt: replayDay0.Add(2 * time.Hour), CreatedAt: replayDay0.Add(4 * time.Hour)}
	txs := []models.StockTransaction{buy, sell}
	SortStockTxs(txs)
	if _, err := CheckLedger(false, Position{10, 150, 150}, txs); !errors.Is(err, ErrLedgerMismatch) {
		t.Fatalf("손익 250 이 0 으로 바뀌므로 ErrLedgerMismatch 여야 한다: %v", err)
	}
}

func TestCheckLedger_손으로평단을고친뒤판기록은_맞지않는다(t *testing.T) {
	// initial 10@100 → 5주@150 매도(250) → 평단을 120 으로 손으로 고침 → 5주@150 매도(150). 다 팔아 종목 없음.
	txs := []models.StockTransaction{
		{ID: "i", Type: models.StockTxBuy, Quantity: 10, Price: 100, Source: models.StockTxSourceInitial, ExecutedAt: replayDay0},
		{ID: "s1", Type: models.StockTxSell, Quantity: 5, Price: 150, AvgPriceAtTx: 100, RealizedPnL: 250, ExecutedAt: replayDay0.AddDate(0, 0, 1)},
		{ID: "s2", Type: models.StockTxSell, Quantity: 5, Price: 150, AvgPriceAtTx: 120, RealizedPnL: 150, ExecutedAt: replayDay0.AddDate(0, 0, 2)},
	}
	if _, err := CheckLedger(false, Position{}, txs); !errors.Is(err, ErrLedgerMismatch) {
		t.Fatalf("두 번째 매도 손익 150 이 250 으로 바뀌므로 ErrLedgerMismatch 여야 한다: %v", err)
	}
	// 손대지 않은 기록이면 통과한다
	txs[2].AvgPriceAtTx, txs[2].RealizedPnL = 100, 250
	if open, err := CheckLedger(false, Position{}, txs); err != nil || open != (Position{}) {
		t.Fatalf("맞는 기록은 통과: %+v %v", open, err)
	}
}

func TestReplay_환율없는USD매수는_계산하지않는다(t *testing.T) {
	// 1주@$277 환율 0 + 1주@$300 환율 1400, 원화 평단을 손으로 400,000 으로 맞춘 종목. 다시 쌓으면 210,000 이 되던 문제.
	txs := []models.StockTransaction{leg(0, models.StockTxBuy, 1, 277, 0), leg(1, models.StockTxBuy, 1, 300, 1400)}
	if _, _, err := Replay(true, Position{}, txs); !errors.Is(err, ErrLedgerFX) {
		t.Fatalf("ErrLedgerFX 여야 한다: %v", err)
	}
	if _, err := CheckLedger(true, Position{2, 288.5, 400_000}, txs); !errors.Is(err, ErrLedgerFX) {
		t.Fatalf("정정 전 검사에서 막혀야 한다: %v", err)
	}
	// 다시 쌓은 원화 평단이 달러 평단과 앞뒤가 안 맞으면 막는다
	if _, _, err := Replay(true, Position{3, 100, 1}, []models.StockTransaction{leg(0, models.StockTxBuy, 1, 100, 1400)}); !errors.Is(err, ErrLedgerFX) {
		t.Fatalf("역산 환율이 범위 밖이면 ErrLedgerFX: %v", err)
	}
}
