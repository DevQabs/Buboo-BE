package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourname/couple-app/internal/auth"
	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
	"github.com/yourname/couple-app/internal/service"
)

// updateStockTx — PUT /api/stocks/transactions/{id}
// 지난 매매 기록의 날짜·수량·단가·환율(USD)을 고치고, 그 종목을 기록 순서대로 다시 계산한다.
func (h *Handler) updateStockTx(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateStockTxRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}
	if (req.Quantity != nil && *req.Quantity <= 0) || (req.Price != nil && *req.Price <= 0) {
		respondError(w, http.StatusBadRequest, fmt.Errorf("수량과 단가는 0보다 커야 해요"))
		return
	}
	h.correctStockTx(w, r, &req)
}

// deleteStockTx — DELETE /api/stocks/transactions/{id}
func (h *Handler) deleteStockTx(w http.ResponseWriter, r *http.Request) {
	h.correctStockTx(w, r, nil)
}

// correctStockTx 는 기록 하나를 고치거나(req) 지우고(req == nil), 그 종목의 모든 기록을
// 시간순으로 다시 적용해 수량·평단·원화 평단과 매도마다의 실현손익을 다시 낸다.
// 기록 이전 보유분(기록 없이 등록한 종목)은 지금 상태에서 거꾸로 구해 그대로 둔다
// (service.Opening). 종목을 잠근 한 DB 트랜잭션이다(stockRepo.Rewrite).
func (h *Handler) correctStockTx(w http.ResponseWriter, r *http.Request, req *models.UpdateStockTxRequest) {
	ctx := r.Context()
	coupleID := auth.CoupleIDFromCtx(ctx)
	id := chi.URLParam(r, "id")

	orig, err := h.stxRepo.GetByID(ctx, coupleID, id)
	if err != nil {
		respondTradeError(w, err)
		return
	}
	if orig.StockAssetID == "" {
		respondError(w, http.StatusConflict, fmt.Errorf("종목 연결이 없는 옛 기록이라 고칠 수 없어요"))
		return
	}
	usd := strings.EqualFold(orig.Currency, "USD")

	// 저축으로 산 기록은 가계부 저축과 짝이라(저축을 지우면 이 기록을 찾아 되돌린다) 날짜·환율만 고친다.
	if orig.Source == models.StockTxSourceSaving &&
		(req == nil || req.Quantity != nil || req.Price != nil) {
		respondError(w, http.StatusConflict, fmt.Errorf("저축으로 산 기록은 날짜·환율만 고칠 수 있어요. 수량·단가를 바꾸거나 지우려면 가계부에서 그 저축을 고쳐 주세요"))
		return
	}

	edited := *orig
	if req != nil {
		if req.Quantity != nil {
			edited.Quantity = *req.Quantity
		}
		if req.Price != nil {
			edited.Price = *req.Price
		}
		dateChanged := false
		if req.Date != nil {
			day, err := time.ParseInLocation(time.DateOnly, *req.Date, models.KST)
			if err != nil {
				respondError(w, http.StatusBadRequest, fmt.Errorf("날짜는 YYYY-MM-DD 로 보내 주세요"))
				return
			}
			if *req.Date > time.Now().In(models.KST).Format(time.DateOnly) {
				respondError(w, http.StatusBadRequest, fmt.Errorf("앞으로의 날짜로는 고칠 수 없어요"))
				return
			}
			// 같은 날 안의 순서는 원래 시각으로 지킨다.
			o := orig.ExecutedAt.In(models.KST)
			edited.ExecutedAt = time.Date(day.Year(), day.Month(), day.Day(), o.Hour(), o.Minute(), o.Second(), o.Nanosecond(), models.KST)
			dateChanged = *req.Date != o.Format(time.DateOnly)
		}
		switch {
		case !usd:
		case req.ExchangeRate != nil:
			if err := service.CheckFX(*req.ExchangeRate); err != nil {
				respondError(w, http.StatusBadRequest, err)
				return
			}
			edited.ExchangeRateAtTx = *req.ExchangeRate
		case dateChanged:
			// 날짜만 바꾸면 그날 환율을 서버가 다시 정한다.
			fx, err := service.ResolveTradeFX(ctx, orig.Currency, edited.ExecutedAt, h.rates)
			if err != nil {
				respondError(w, http.StatusBadRequest, err)
				return
			}
			edited.ExchangeRateAtTx = fx
		}
	}

	var removed bool
	asset, err := h.stockRepo.Rewrite(ctx, orig.StockAssetID, coupleID, func(a *models.StockAsset, txs []models.StockTransaction) (repository.StockTxChanges, error) {
		var ch repository.StockTxChanges
		i := slices.IndexFunc(txs, func(t models.StockTransaction) bool { return t.ID == id })
		if i < 0 {
			return ch, repository.ErrNotFound // 그 사이 지워졌다
		}
		var cur service.Position
		if a != nil {
			cur = service.Position{Qty: a.Quantity, Avg: a.AveragePrice, AvgKRW: a.AvgKRWPrice}
		}
		open, err := service.Opening(usd, cur, txs)
		if err != nil {
			return ch, statusError{http.StatusConflict, err}
		}
		next := slices.Clone(txs)
		if req == nil {
			next = slices.Delete(next, i, i+1)
			ch.Delete = []string{id}
		} else {
			next[i] = edited
			service.SortStockTxs(next)
		}
		final, out, err := service.Replay(usd, open, next)
		switch {
		case errors.Is(err, service.ErrLedgerFX):
			return ch, statusError{http.StatusUnprocessableEntity, err}
		case err != nil:
			return ch, statusError{http.StatusConflict, err}
		}
		if a == nil && final.Qty >= models.QtyEpsilon {
			return ch, statusError{http.StatusConflict, fmt.Errorf("다 판 종목은 수량이 남도록 고칠 수 없어요. 남는 주식은 종목을 다시 추가해 주세요")}
		}
		if a != nil {
			a.Quantity, a.AveragePrice, a.AvgKRWPrice = final.Qty, final.Avg, final.AvgKRW
			removed = final.Qty < models.QtyEpsilon
		}
		ch.Update = out
		return ch, nil
	})
	if err != nil {
		respondTradeError(w, err)
		return
	}
	if removed || asset == nil {
		respondJSON(w, http.StatusOK, map[string]any{"asset": nil, "removed": removed})
		return
	}
	respondJSON(w, http.StatusOK, map[string]any{"asset": asset, "removed": false})
}
