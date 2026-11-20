// stock_replay.go — 매매 기록을 시간순으로 다시 적용해 보유 수량·평단·실현손익을 낸다.
//
// 평균법: 매수는 평단을 섞고, 매도는 평단을 그대로 두고 (매도가 − 평단) × 수량을
// 실현손익으로 남긴다. USD 종목의 실현손익은 원화다: (매도가 × 매도 환율 − 원화 평단) × 수량.
//
// 기록보다 먼저 있던 보유분(createStock 이 기록을 남기기 전 등록한 종목, 앱 도입 전 보유분)은
// 기록에 없다. 그래서 처음부터(0주) 다시 쌓지 않고, 지금 보유 상태에서 기록을 최근 것부터
// 거꾸로 되돌려 "기록 이전 보유분"(Opening)을 구한 뒤 그 위에 정정된 기록을 다시 쌓는다.
// 정정이 없으면 Replay(Opening(지금)) 은 지금 상태로 돌아온다. 기존 데이터를 0 으로 만들지 않는다.
package service

import (
	"errors"
	"fmt"
	"sort"

	"github.com/yourname/couple-app/internal/models"
)

// Position 은 한 종목의 보유 상태다. Avg 는 원래 통화 평단, AvgKRW 는 원화 평단(KRW 종목은 Avg 와 같다).
type Position struct {
	Qty, Avg, AvgKRW float64
}

var (
	// ErrLedgerNegative: 정정대로면 어느 시점에 보유 수량이 0 아래로 내려간다.
	ErrLedgerNegative = errors.New("이렇게 고치면 보유 수량이 0 아래로 내려가는 때가 생겨요")
	// ErrLedgerMismatch: 지금 보유 정보와 기록이 맞지 않아 기록 이전 보유분을 구할 수 없다(기록 밖에서 수량을 고친 경우).
	ErrLedgerMismatch = errors.New("지금 보유 수량과 매매 기록이 맞지 않아 자동으로 다시 계산할 수 없어요")
	// ErrLedgerFX: USD 매도를 원화 손익으로 계산할 수 없다(환율 없음·원화 평단 손상).
	ErrLedgerFX = errors.New("원화 손익을 계산할 수 없는 매도 기록이 있어요")
)

// SortStockTxs 는 기록을 거래일 → 기록 시각 → id 순으로 정렬한다. Opening 과 Replay 는 같은 순서를 쓴다.
func SortStockTxs(txs []models.StockTransaction) {
	sort.SliceStable(txs, func(i, j int) bool {
		a, b := txs[i], txs[j]
		if !a.ExecutedAt.Equal(b.ExecutedAt) {
			return a.ExecutedAt.Before(b.ExecutedAt)
		}
		if !a.CreatedAt.Equal(b.CreatedAt) {
			return a.CreatedAt.Before(b.CreatedAt)
		}
		return a.ID < b.ID
	})
}

// Opening 은 정렬된 기록 txs 이전의 보유분이다. cur(지금 상태)에서 기록을 최근 것부터 되돌린다.
// 다 판 종목은 cur 가 0 이다. 0주에서 매도를 되돌릴 때는 그 매도 기록의 평단으로 평단을 되살린다.
func Opening(usd bool, cur Position, txs []models.StockTransaction) (Position, error) {
	p := cur
	for i := len(txs) - 1; i >= 0; i-- {
		t := txs[i]
		switch t.Type {
		case models.StockTxSell:
			if p.Qty < models.QtyEpsilon {
				p.Avg, p.AvgKRW = t.AvgPriceAtTx, t.AvgPriceAtTx
				if usd {
					p.AvgKRW = 0
					if t.ExchangeRateAtTx > 0 && t.Quantity > 0 {
						p.AvgKRW = t.Price*t.ExchangeRateAtTx - t.RealizedPnL/t.Quantity
					}
				}
			}
			p.Qty += t.Quantity
		case models.StockTxBuy:
			prev := p.Qty - t.Quantity
			if prev < -models.QtyEpsilon {
				return Position{}, ErrLedgerMismatch
			}
			if prev < models.QtyEpsilon {
				p = Position{}
				continue
			}
			p.Avg = (p.Avg*p.Qty - t.Quantity*t.Price) / prev
			if usd {
				p.AvgKRW = (p.AvgKRW*p.Qty - t.Quantity*t.Price*t.ExchangeRateAtTx) / prev
			} else {
				p.AvgKRW = p.Avg
			}
			p.Qty = prev
		}
	}
	if p.Qty < models.QtyEpsilon {
		return Position{}, nil
	}
	if p.Avg < 0 || p.AvgKRW < 0 {
		return Position{}, ErrLedgerMismatch
	}
	return p, nil
}

// Replay 는 open 위에 정렬된 기록 txs 를 다시 적용한다. 각 기록의 평단(avg_price_at_tx)과
// 매도 실현손익을 다시 채운 사본과 최종 보유 상태를 돌려준다.
func Replay(usd bool, open Position, txs []models.StockTransaction) (Position, []models.StockTransaction, error) {
	p := open
	out := make([]models.StockTransaction, len(txs))
	for i, t := range txs {
		switch t.Type {
		case models.StockTxBuy:
			if p.Qty < models.QtyEpsilon {
				p = Position{}
			}
			n := p.Qty + t.Quantity
			p.Avg = (p.Avg*p.Qty + t.Quantity*t.Price) / n
			if usd {
				p.AvgKRW = (p.AvgKRW*p.Qty + t.Quantity*t.Price*t.ExchangeRateAtTx) / n
			} else {
				p.AvgKRW = p.Avg
			}
			p.Qty = n
			t.AvgPriceAtTx = p.Avg
		case models.StockTxSell:
			day := t.ExecutedAt.In(models.KST).Format("2006-01-02")
			if t.Quantity > p.Qty+models.QtyEpsilon {
				return Position{}, nil, fmt.Errorf("%w (%s 매도 %.4g주, 그때 보유 %.4g주)", ErrLedgerNegative, day, t.Quantity, p.Qty)
			}
			t.AvgPriceAtTx = p.Avg
			if usd {
				if t.ExchangeRateAtTx <= 0 {
					return Position{}, nil, fmt.Errorf("%w: %s 매도에 환율이 없어요. 그 매도의 환율을 먼저 넣어 주세요", ErrLedgerFX, day)
				}
				if err := ValidateKRWCostBasis(p.Avg, p.AvgKRW); err != nil {
					return Position{}, nil, fmt.Errorf("%w: %s 매도 — %v", ErrLedgerFX, day, err)
				}
				t.RealizedPnL = (t.Price*t.ExchangeRateAtTx - p.AvgKRW) * t.Quantity
			} else {
				t.RealizedPnL = (t.Price - p.Avg) * t.Quantity
			}
			p.Qty -= t.Quantity
			if p.Qty < models.QtyEpsilon {
				p = Position{}
			}
		}
		out[i] = t
	}
	return p, out, nil
}
