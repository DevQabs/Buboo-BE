// Package handler wires HTTP routes to the repository layer using chi router.
// All responses are JSON; errors return a structured { "error": "..." } body.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"
	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
	"github.com/yourname/couple-app/internal/service"
	supstorage "github.com/yourname/couple-app/internal/storage"
)

// ─────────────────────────────────────────────
//  Handler struct
// ─────────────────────────────────────────────

type Handler struct {
	txRepo       repository.TransactionRepository
	stockRepo    repository.StockRepository
	stxRepo      repository.StockTransactionRepository
	userRepo     repository.UserRepository
	assetRepo    repository.OtherAssetRepository
	feRepo       repository.FixedExpenseRepository
	divRepo      repository.DividendRepository
	scheduleRepo repository.ScheduleRepository
	diaryRepo    repository.DiaryRepository
	catRepo      repository.CategoryRepository
	priceSvc     *service.PriceService
	savingSvc    *service.SavingService
	coupleID     string
	allowOrigins []string
	uploadsDir   string
	stor         *supstorage.SupabaseStorage
}

func New(
	txRepo repository.TransactionRepository,
	stockRepo repository.StockRepository,
	stxRepo repository.StockTransactionRepository,
	userRepo repository.UserRepository,
	assetRepo repository.OtherAssetRepository,
	feRepo repository.FixedExpenseRepository,
	divRepo repository.DividendRepository,
	scheduleRepo repository.ScheduleRepository,
	diaryRepo repository.DiaryRepository,
	catRepo repository.CategoryRepository,
	priceSvc *service.PriceService,
	savingSvc *service.SavingService,
	coupleID string,
	allowOrigins []string,
	uploadsDir string,
	stor *supstorage.SupabaseStorage,
) *Handler {
	return &Handler{
		txRepo:       txRepo,
		stockRepo:    stockRepo,
		stxRepo:      stxRepo,
		userRepo:     userRepo,
		assetRepo:    assetRepo,
		feRepo:       feRepo,
		divRepo:      divRepo,
		scheduleRepo: scheduleRepo,
		diaryRepo:    diaryRepo,
		catRepo:      catRepo,
		priceSvc:     priceSvc,
		savingSvc:    savingSvc,
		coupleID:     coupleID,
		allowOrigins: allowOrigins,
		uploadsDir:   uploadsDir,
		stor:         stor,
	}
}

// ─────────────────────────────────────────────
//  Router
// ─────────────────────────────────────────────

func (h *Handler) NewRouter() chi.Router {
	r := chi.NewRouter()

	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(middleware.RequestID)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   h.allowOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		respondJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	r.Head("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	r.Route("/api", func(r chi.Router) {

		// Users
		r.Get("/users", h.listUsers)

		// Couple
		r.Get("/couple", h.getCouple)
		r.Put("/couple", h.updateCouple)

		// Categories
		r.Get("/categories", h.getCategories)
		r.Put("/categories", h.updateCategories)

		// Transactions
		r.Route("/transactions", func(r chi.Router) {
			r.Get("/", h.listTransactions)
			r.Post("/", h.createTransaction)
			// Calendar summary — must be registered BEFORE /{id} to avoid routing conflict
			r.Get("/calendar-summary", h.calendarSummary) // ?year=&month=
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.getTransaction)
				r.Put("/", h.updateTransaction)
				r.Delete("/", h.deleteTransaction)
			})
		})

		// Monthly summary
		r.Get("/summary", h.monthlySummary)

		// Stocks
		r.Route("/stocks", func(r chi.Router) {
			r.Get("/", h.listStocks)
			r.Post("/", h.createStock)
			r.Get("/portfolio", h.portfolio)
			r.Get("/exchange-rate", h.exchangeRate)
			r.Post("/refresh", h.refreshPrices)
			r.Get("/tax", h.annualTax)           // GET /api/stocks/tax?year=2026
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.getStock)
				r.Put("/", h.updateStock)
				r.Delete("/", h.deleteStock)
				r.Post("/buy", h.buyStock)        // 매수
				r.Post("/sell", h.sellStock)      // 매도
				r.Get("/tax-check", h.taxCheck)   // 삭제 전 경고 여부 확인
			})
		})

		// Other Assets (부동산, 예금, 가상화폐, 차량 등)
		r.Route("/assets", func(r chi.Router) {
			r.Get("/", h.listAssets)           // 전체 목록 (type 쿼리 파라미터로 필터)
			r.Post("/", h.createAsset)          // 자산 추가
			r.Get("/net-worth", h.netWorth)     // 순자산 요약 (주식 + 기타)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.getAsset)          // 단건 조회
				r.Put("/", h.updateAsset)       // 수정 (partial update)
				r.Delete("/", h.deleteAsset)    // 삭제
				r.Post("/loan-expense", h.createLoanFixedExpense) // 대출 → 고정비 생성
			})
		})

		// Dividends (배당 이벤트)
		r.Route("/dividends", func(r chi.Router) {
			r.Get("/", h.listDividends)              // 전체 목록
			r.Post("/", h.createDividend)            // 배당 이벤트 등록
			r.Get("/summary", h.dividendSummary)     // 연간 요약 (?year=)
			r.Route("/{id}", func(r chi.Router) {
				r.Delete("/", h.deleteDividend)      // 삭제
				r.Post("/apply", h.applyDividend)    // 가계부 수입 반영
			})
		})

		// Fixed Expenses (고정비 — 정기 지출 템플릿)
		r.Route("/fixed-expenses", func(r chi.Router) {
			r.Get("/", h.listFixedExpenses)
			r.Post("/", h.createFixedExpense)
			r.Get("/summary", h.fixedExpenseSummary)
			r.Post("/apply", h.applyFixedExpenses)
			r.Route("/{id}", func(r chi.Router) {
				r.Put("/", h.updateFixedExpense)
				r.Delete("/", h.deleteFixedExpense)
			})
		})

		// Schedules (일정)
		r.Route("/schedules", func(r chi.Router) {
			r.Get("/", h.listSchedules)
			r.Post("/", h.createSchedule)
			r.Route("/{id}", func(r chi.Router) {
				r.Put("/", h.updateSchedule)
				r.Delete("/", h.deleteSchedule)
			})
		})

		// Diaries (일기)
		r.Route("/diaries", func(r chi.Router) {
			r.Get("/", h.listDiaries)
			r.Post("/", h.createDiary)
			r.Route("/{id}", func(r chi.Router) {
				r.Put("/", h.updateDiary)
				r.Delete("/", h.deleteDiary)
				r.Post("/photos", h.uploadDiaryPhoto)
				r.Delete("/photos/{filename}", h.deleteDiaryPhoto)
			})
		})
	})

	// Static file serving for uploaded photos
	r.Get("/uploads/{filename}", h.serveUpload)

	return r
}

// ─────────────────────────────────────────────
//  Transaction handlers
// ─────────────────────────────────────────────

func (h *Handler) listTransactions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	yearStr, monthStr := q.Get("year"), q.Get("month")
	if yearStr != "" && monthStr != "" {
		year, errY := strconv.Atoi(yearStr)
		month, errM := strconv.Atoi(monthStr)
		if errY == nil && errM == nil {
			txns, err := h.txRepo.ListByMonth(r.Context(), h.coupleID, year, month)
			if err != nil {
				respondError(w, http.StatusInternalServerError, err)
				return
			}
			if txns == nil {
				txns = make([]models.Transaction, 0)
			}
			respondJSON(w, http.StatusOK, txns)
			return
		}
	}
	txns, err := h.txRepo.ListByCouple(r.Context(), h.coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, txns)
}

func (h *Handler) createTransaction(w http.ResponseWriter, r *http.Request) {
	var req models.CreateTransactionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}

	// Saving transactions go through SavingService for atomic asset update.
	if req.Type == "saving" {
		if req.SavingLink == nil {
			respondError(w, http.StatusBadRequest, fmt.Errorf("saving_link is required when type=saving"))
			return
		}
		if req.Amount <= 0 {
			respondError(w, http.StatusBadRequest, fmt.Errorf("amount must be > 0"))
			return
		}
		created, err := h.savingSvc.ApplySaving(r.Context(), service.ApplySavingRequest{
			UserID:        req.UserID,
			AmountKRW:     req.Amount,
			Title:         req.Title,
			Memo:          req.Memo,
			Date:          req.Date,
			PaymentMethod: req.PaymentMethod,
			IsFixed:       req.IsFixed,
			Tags:          req.Tags,
			Link:          *req.SavingLink,
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
		respondJSON(w, http.StatusCreated, created)
		return
	}

	tx := &models.Transaction{
		CoupleID:      h.coupleID,
		UserID:        req.UserID,
		Type:          req.Type,
		Amount:        req.Amount,
		Currency:      req.Currency,
		Category:      req.Category,
		Subcategory:   req.Subcategory,
		Title:         req.Title,
		Memo:          req.Memo,
		Date:          req.Date,
		PaymentMethod: req.PaymentMethod,
		IsFixed:       req.IsFixed,
		Tags:          req.Tags,
		Location:      req.Location,
	}
	created, err := h.txRepo.Create(r.Context(), tx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusCreated, created)
}

func (h *Handler) getTransaction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	tx, err := h.txRepo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	respondJSON(w, http.StatusOK, tx)
}

func (h *Handler) updateTransaction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var tx models.Transaction
	if err := json.NewDecoder(r.Body).Decode(&tx); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}
	tx.ID = id
	updated, err := h.txRepo.Update(r.Context(), &tx)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

func (h *Handler) deleteTransaction(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.txRepo.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// calendarSummary: GET /api/transactions/calendar-summary?year=2026&month=5
//
// Returns pre-aggregated daily totals (GROUP BY date) so the client can render
// the calendar grid in O(days) without re-summing on the frontend.
// Also includes fixed-expense transfer days and dividend payment days as event dots.
func (h *Handler) calendarSummary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	now := time.Now()
	year := now.Year()
	month := int(now.Month())
	if y := q.Get("year"); y != "" {
		if v, err := strconv.Atoi(y); err == nil { year = v }
	}
	if m := q.Get("month"); m != "" {
		if v, err := strconv.Atoi(m); err == nil { month = v }
	}

	// ── 1. Aggregate transactions by date (GROUP BY equivalent) ──────────────
	txns, err := h.txRepo.ListByMonth(r.Context(), h.coupleID, year, month)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	type dayAccum struct {
		totalExpense     int64
		totalIncome      int64
		transactionCount int
	}
	dayMap := make(map[string]*dayAccum)
	for _, tx := range txns {
		dateKey := tx.Date.Format("2006-01-02")
		if dayMap[dateKey] == nil {
			dayMap[dateKey] = &dayAccum{}
		}
		dayMap[dateKey].transactionCount++
		switch tx.Type {
		case "expense":
			dayMap[dateKey].totalExpense += tx.Amount
		case "income":
			// 배당 태그 income은 dividend event로 별도 표시 — total_income 제외
			isDividend := false
			for _, tag := range tx.Tags {
				if tag == "배당" {
					isDividend = true
					break
				}
			}
			if !isDividend {
				dayMap[dateKey].totalIncome += tx.Amount
			}
		}
	}

	days := make([]models.CalendarDay, 0, len(dayMap))
	for dateKey, acc := range dayMap {
		days = append(days, models.CalendarDay{
			Date:             dateKey,
			TotalExpense:     acc.totalExpense,
			TotalIncome:      acc.totalIncome,
			TransactionCount: acc.transactionCount,
		})
	}
	// Sort ascending by date for predictable ordering.
	sort.Slice(days, func(i, j int) bool { return days[i].Date < days[j].Date })

	// ── 2. Fixed expense event dots (이체 예정일, 모두 표시) ─────────────────────
	events := make([]models.CalendarEvent, 0)

	fes, _ := h.feRepo.ListByCouple(r.Context(), h.coupleID)
	for _, fe := range fes {
		if !fe.IsActive {
			continue
		}
		// Clamp day to 28 for months with fewer days.
		day := fe.DayOfMonth
		if day > 28 {
			day = 28
		}
		dateKey := fmt.Sprintf("%04d-%02d-%02d", year, month, day)
		events = append(events, models.CalendarEvent{
			Date:   dateKey,
			Type:   "fixed_expense",
			Title:  fe.Title,
			Amount: fe.Amount,
		})
	}

	// ── 3. Dividend event dots (배당 지급일) ──────────────────────────────────
	divs, _ := h.divRepo.ListByYear(r.Context(), h.coupleID, year)
	for _, d := range divs {
		if int(d.PaymentDate.Month()) == month {
			events = append(events, models.CalendarEvent{
				Date:   d.PaymentDate.Format("2006-01-02"),
				Type:   "dividend",
				Title:  d.Name + " 배당",
				Amount: d.AmountKRW,
			})
		}
	}

	respondJSON(w, http.StatusOK, models.CalendarSummaryResponse{
		Year:   year,
		Month:  month,
		Days:   days,
		Events: events,
	})
}

func (h *Handler) monthlySummary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	now := time.Now()

	// Custom date range: ?start_date=2026-04-25&end_date=2026-05-25
	startStr := q.Get("start_date")
	endStr := q.Get("end_date")

	if startStr != "" && endStr != "" {
		start, err1 := time.Parse("2006-01-02", startStr)
		end, err2 := time.Parse("2006-01-02", endStr)
		if err1 != nil || err2 != nil {
			respondError(w, http.StatusBadRequest, fmt.Errorf("invalid date format: use YYYY-MM-DD"))
			return
		}
		summary, err := h.txRepo.SummaryByDateRange(r.Context(), h.coupleID, start, end)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
		if couple, err2 := h.userRepo.GetCouple(r.Context(), h.coupleID); err2 == nil {
			summary.BudgetLimit = couple.MonthlyBudget
		}
		respondJSON(w, http.StatusOK, summary)
		return
	}

	// Calendar month fallback: ?year=2026&month=5
	year, _ := strconv.Atoi(q.Get("year"))
	month, _ := strconv.Atoi(q.Get("month"))
	if year == 0 {
		year = now.Year()
	}
	if month == 0 {
		month = int(now.Month())
	}
	summary, err := h.txRepo.MonthlySummary(r.Context(), h.coupleID, year, month)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	if couple, err2 := h.userRepo.GetCouple(r.Context(), h.coupleID); err2 == nil {
		summary.BudgetLimit = couple.MonthlyBudget
	}
	respondJSON(w, http.StatusOK, summary)
}

// ─────────────────────────────────────────────
//  Stock handlers
// ─────────────────────────────────────────────

func (h *Handler) listStocks(w http.ResponseWriter, r *http.Request) {
	assets, err := h.stockRepo.ListByCouple(r.Context(), h.coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, assets)
}

func (h *Handler) createStock(w http.ResponseWriter, r *http.Request) {
	var req models.CreateStockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}

	// ── 필수 필드 검증 ────────────────────────────────────────────────────────
	var errs []string
	if req.UserID == "" {
		errs = append(errs, "user_id is required")
	}
	if req.Symbol == "" {
		errs = append(errs, "symbol is required")
	}
	if req.Exchange == "" {
		errs = append(errs, "exchange is required")
	}
	if req.Name == "" {
		errs = append(errs, "name is required")
	}
	if req.Quantity <= 0 {
		errs = append(errs, "quantity must be > 0")
	}
	if req.AveragePrice <= 0 {
		errs = append(errs, "average_price must be > 0")
	}
	if req.Currency == "" {
		errs = append(errs, "currency is required (KRW or USD)")
	}
	if len(errs) > 0 {
		respondJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "validation failed",
			"fields": errs,
		})
		return
	}

	now := time.Now().UTC()
	purchasedAt := now
	if req.PurchasedAt != nil {
		purchasedAt = *req.PurchasedAt
	}

	asset := &models.StockAsset{
		CoupleID:     h.coupleID,
		UserID:       req.UserID,
		Symbol:       strings.ToUpper(strings.TrimSpace(req.Symbol)),
		Exchange:     strings.ToUpper(strings.TrimSpace(req.Exchange)),
		Name:         req.Name,
		NameEn:       req.NameEn,
		Quantity:     req.Quantity,
		AveragePrice: req.AveragePrice,
		Currency:     strings.ToUpper(strings.TrimSpace(req.Currency)),
		Sector:       req.Sector,
		Memo:         req.Memo,
		PurchasedAt:  purchasedAt,
	}

	created, err := h.stockRepo.Create(r.Context(), asset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusCreated, created)
}

func (h *Handler) getStock(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	asset, err := h.stockRepo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	respondJSON(w, http.StatusOK, asset)
}

func (h *Handler) updateStock(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	// Load existing asset first
	existing, err := h.stockRepo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}

	var req models.UpdateStockRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid JSON: %w", err))
		return
	}

	// Partial update — only non-nil fields are applied
	if req.UserID != nil {
		existing.UserID = *req.UserID
	}
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Quantity != nil {
		if *req.Quantity <= 0 {
			respondError(w, http.StatusBadRequest, fmt.Errorf("quantity must be > 0"))
			return
		}
		existing.Quantity = *req.Quantity
	}
	if req.AveragePrice != nil {
		if *req.AveragePrice <= 0 {
			respondError(w, http.StatusBadRequest, fmt.Errorf("average_price must be > 0"))
			return
		}
		existing.AveragePrice = *req.AveragePrice
	}
	if req.Sector != nil {
		existing.Sector = *req.Sector
	}
	if req.Memo != nil {
		existing.Memo = *req.Memo
	}

	updated, err := h.stockRepo.Update(r.Context(), existing)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

func (h *Handler) deleteStock(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	// force=true bypasses the tax-warning check (user confirmed via modal)
	if r.URL.Query().Get("force") != "true" {
		asset, err := h.stockRepo.GetByID(r.Context(), id)
		if err != nil {
			respondError(w, http.StatusNotFound, err)
			return
		}
		hasSell, _ := h.stxRepo.HasSellInYear(r.Context(), h.coupleID, asset.Symbol, time.Now().Year())
		if hasSell {
			// 409 signals the frontend to show the tax-warning modal
			respondJSON(w, http.StatusConflict, map[string]interface{}{
				"tax_warning":          true,
				"has_sell_current_year": true,
				"year":                 time.Now().Year(),
			})
			return
		}
	}
	if err := h.stockRepo.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// buyStock — POST /api/stocks/{id}/buy
// Increases quantity, recalculates weighted-average price, logs immutable transaction.
func (h *Handler) buyStock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var req models.BuyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}
	if req.Quantity <= 0 || req.Price <= 0 {
		respondError(w, http.StatusBadRequest, fmt.Errorf("quantity and price must be > 0"))
		return
	}

	asset, err := h.stockRepo.GetByID(ctx, id)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}

	// Weighted-average cost basis
	newQty := asset.Quantity + req.Quantity
	newAvg := (asset.Quantity*asset.AveragePrice + req.Quantity*req.Price) / newQty
	asset.Quantity = newQty
	asset.AveragePrice = newAvg

	updated, err := h.stockRepo.Update(ctx, asset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	// Append immutable log
	stx := &models.StockTransaction{
		CoupleID:     h.coupleID,
		UserID:       asset.UserID,
		StockAssetID: asset.ID,
		Symbol:       asset.Symbol,
		Exchange:     asset.Exchange,
		Name:         asset.Name,
		Type:         models.StockTxBuy,
		Quantity:     req.Quantity,
		Price:        req.Price,
		Currency:     asset.Currency,
		AvgPriceAtTx: newAvg,
		RealizedPnL:  0,
		Memo:         req.Memo,
	}
	if _, err := h.stxRepo.Create(ctx, stx); err != nil {
		// Log failure is non-fatal for the user — asset is already updated
		fmt.Printf("warn: stx log failed: %v\n", err)
	}

	respondJSON(w, http.StatusOK, updated)
}

// sellStock — POST /api/stocks/{id}/sell
// Decreases quantity, calculates realized P&L, logs immutable transaction.
// If quantity reaches 0, the stock asset is hard-deleted (log is kept).
func (h *Handler) sellStock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	var req models.SellRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}
	if req.Quantity <= 0 || req.Price <= 0 {
		respondError(w, http.StatusBadRequest, fmt.Errorf("quantity and price must be > 0"))
		return
	}

	asset, err := h.stockRepo.GetByID(ctx, id)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	if req.Quantity > asset.Quantity {
		respondError(w, http.StatusBadRequest, fmt.Errorf("sell quantity (%.4f) exceeds holdings (%.4f)", req.Quantity, asset.Quantity))
		return
	}

	// Realized P&L = (sell_price - avg_cost) × quantity
	realizedPnL := (req.Price - asset.AveragePrice) * req.Quantity

	// Append immutable log BEFORE mutating state
	stx := &models.StockTransaction{
		CoupleID:     h.coupleID,
		UserID:       asset.UserID,
		StockAssetID: asset.ID,
		Symbol:       asset.Symbol,
		Exchange:     asset.Exchange,
		Name:         asset.Name,
		Type:         models.StockTxSell,
		Quantity:     req.Quantity,
		Price:        req.Price,
		Currency:     asset.Currency,
		AvgPriceAtTx: asset.AveragePrice,
		RealizedPnL:  realizedPnL,
		Memo:         req.Memo,
	}
	if _, err := h.stxRepo.Create(ctx, stx); err != nil {
		fmt.Printf("warn: stx log failed: %v\n", err)
	}

	newQty := asset.Quantity - req.Quantity

	if newQty == 0 {
		// 잔고 0 → 포트폴리오에서 즉시 제거 (이력은 보존)
		if err := h.stockRepo.Delete(ctx, id); err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{
			"removed":       true,
			"realized_pnl":  realizedPnL,
			"symbol":        asset.Symbol,
		})
		return
	}

	// avg_price stays the same after sell (average cost method)
	asset.Quantity = newQty
	updated, err := h.stockRepo.Update(ctx, asset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]interface{}{
		"asset":        updated,
		"realized_pnl": realizedPnL,
	})
}

// taxCheck — GET /api/stocks/{id}/tax-check
// Returns whether the asset has any SELL transactions in the current year.
// Used by the frontend to decide whether to show the tax-warning delete modal.
func (h *Handler) taxCheck(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	asset, err := h.stockRepo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	year := time.Now().Year()
	hasSell, err := h.stxRepo.HasSellInYear(r.Context(), h.coupleID, asset.Symbol, year)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, models.TaxCheckResponse{
		HasSellCurrentYear: hasSell,
		Year:               year,
	})
}

// annualTax — GET /api/stocks/tax?year=2026
// Returns realized P&L and estimated 22% tax aggregated by symbol.
func (h *Handler) annualTax(w http.ResponseWriter, r *http.Request) {
	yearStr := r.URL.Query().Get("year")
	year := time.Now().Year()
	if yearStr != "" {
		if y, err := strconv.Atoi(yearStr); err == nil {
			year = y
		}
	}

	bySymbol, err := h.stxRepo.AnnualSummary(r.Context(), h.coupleID, year)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	const taxRate = 0.22
	var totalPnL float64
	for _, s := range bySymbol {
		totalPnL += s.RealizedPnL
	}
	taxable := totalPnL
	if taxable < 0 {
		taxable = 0
	}
	summary := models.AnnualTaxSummary{
		Year:             year,
		CoupleID:         h.coupleID,
		TotalRealizedPnL: totalPnL,
		TaxableGain:      taxable,
		EstimatedTax:     taxable * taxRate,
		TaxRate:          taxRate,
		BySymbol:         bySymbol,
	}
	respondJSON(w, http.StatusOK, summary)
}

// portfolio returns all stock assets with live prices, KRW conversion, and a summary.
//
// Response shape:
//
//	{
//	  "items": [ ...StockAssetWithPrice ],
//	  "summary": { ...PortfolioSummary }
//	}
func (h *Handler) portfolio(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// ── 1. Fetch USD/KRW exchange rate ────────────────────────────────────────
	usdKRW, fxErr := h.priceSvc.FetchUSDKRW(ctx)
	fxSource := "live"
	if fxErr != nil || usdKRW == 0 {
		usdKRW = service.FallbackUSDKRW
		fxSource = "fallback"
	}

	// ── 2. Load holdings ──────────────────────────────────────────────────────
	assets, err := h.stockRepo.ListByCouple(ctx, h.coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	// ── 3. Enrich each holding with live price ────────────────────────────────
	items := make([]models.StockAssetWithPrice, 0, len(assets))
	var totalValueKRW, totalCostKRW float64

	for _, a := range assets {
		item := models.StockAssetWithPrice{StockAsset: a}

		// Try live price first
		snap, liveErr := h.priceSvc.FetchPrice(ctx, a.Symbol, a.Exchange)
		if liveErr != nil {
			// Fall back to stored snapshot
			snap, _ = h.stockRepo.GetPriceSnapshot(ctx, a.Symbol)
			item.PriceSource = "cached"
		} else {
			item.PriceSource = "live"
			// Persist live price so offline fallback stays fresh
			_ = h.stockRepo.UpsertPriceSnapshot(ctx, snap)
		}

		if snap != nil {
			item.CurrentPrice = snap.Price
			item.CurrentValue = snap.Price * a.Quantity
			item.Change = snap.Change
			item.ChangePercent = snap.ChangePercent
			item.PriceUpdatedAt = snap.SnapshottedAt

			costBasis := a.AveragePrice * a.Quantity
			item.ProfitLoss = item.CurrentValue - costBasis
			if costBasis > 0 {
				item.ProfitLossPct = (item.ProfitLoss / costBasis) * 100
			}

			// KRW conversion
			switch strings.ToUpper(a.Currency) {
			case "USD":
				item.CurrentValueKRW = item.CurrentValue * usdKRW
				item.ProfitLossKRW = item.ProfitLoss * usdKRW
				item.ExchangeRate = usdKRW
				totalCostKRW += costBasis * usdKRW
			default: // KRW
				item.CurrentValueKRW = item.CurrentValue
				item.ProfitLossKRW = item.ProfitLoss
				totalCostKRW += costBasis
			}
			totalValueKRW += item.CurrentValueKRW
		}

		items = append(items, item)
	}

	// ── 4. Build summary ──────────────────────────────────────────────────────
	totalProfitKRW := totalValueKRW - totalCostKRW
	var totalProfitPct float64
	if totalCostKRW > 0 {
		totalProfitPct = (totalProfitKRW / totalCostKRW) * 100
	}

	summary := models.PortfolioSummary{
		TotalValueKRW:  totalValueKRW,
		TotalCostKRW:   totalCostKRW,
		TotalProfitKRW: totalProfitKRW,
		TotalProfitPct: totalProfitPct,
		USDKRW:         usdKRW,
		FXSource:       fxSource,
		CalculatedAt:   time.Now().UTC(),
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"items":   items,
		"summary": summary,
	})
}

// exchangeRate returns the current USD/KRW rate.
// GET /api/stocks/exchange-rate
func (h *Handler) exchangeRate(w http.ResponseWriter, r *http.Request) {
	rate, err := h.priceSvc.FetchUSDKRW(r.Context())
	source := "live"
	if err != nil || rate == 0 {
		rate = service.FallbackUSDKRW
		source = "fallback"
	}
	respondJSON(w, http.StatusOK, map[string]any{
		"from":       "USD",
		"to":         "KRW",
		"rate":       rate,
		"source":     source,
		"fetched_at": time.Now().UTC(),
	})
}

// refreshPrices invalidates the in-memory price cache, forcing the next
// portfolio request to fetch fresh data from Yahoo Finance.
// POST /api/stocks/refresh
func (h *Handler) refreshPrices(w http.ResponseWriter, r *http.Request) {
	h.priceSvc.InvalidateAll()
	respondJSON(w, http.StatusOK, map[string]string{"status": "cache cleared"})
}

// ─────────────────────────────────────────────
//  Other Asset handlers  (/api/assets)
// ─────────────────────────────────────────────

// calcLoanMonthlyPayment mirrors the frontend calcLoanPayment logic.
func calcLoanMonthlyPayment(balanceKRW int64, interestRate *float64, loanType string, maturityDate *time.Time) int64 {
	if balanceKRW == 0 || interestRate == nil || maturityDate == nil {
		return 0
	}
	r := *interestRate / 100.0 / 12.0
	remaining := math.Max(1, math.Round(maturityDate.Sub(time.Now()).Hours()/24/30.44))
	n := int(remaining)
	switch loanType {
	case "만기일시상환":
		return int64(math.Round(float64(balanceKRW) * r))
	case "원리금균등상환":
		if r == 0 {
			return balanceKRW / int64(n)
		}
		factor := math.Pow(1+r, float64(n))
		return int64(math.Round(float64(balanceKRW) * r * factor / (factor - 1)))
	case "원금균등상환":
		return int64(math.Round(float64(balanceKRW)/float64(n) + float64(balanceKRW)*r))
	}
	return 0
}

// applyUSDCashRate rewrites ValueKRW for USD cash assets using the live rate.
func applyUSDCashRate(assets []models.OtherAsset, usdKRW float64) {
	for i := range assets {
		if assets[i].AssetType == models.AssetTypeCash && strings.ToUpper(assets[i].Currency) == "USD" && assets[i].ValueUSD != nil {
			assets[i].ValueKRW = int64(*assets[i].ValueUSD * usdKRW)
		}
	}
}

// listAssets returns all other assets for the couple.
// Optional query param: ?type=부동산  (filters by asset_type)
func (h *Handler) listAssets(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	assetType := r.URL.Query().Get("type")

	var assets []models.OtherAsset
	var err error
	if assetType != "" {
		assets, err = h.assetRepo.ListByType(ctx, h.coupleID, models.OtherAssetType(assetType))
	} else {
		assets, err = h.assetRepo.ListByCouple(ctx, h.coupleID)
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	usdKRW, fxErr := h.priceSvc.FetchUSDKRW(ctx)
	if fxErr != nil || usdKRW == 0 {
		usdKRW = service.FallbackUSDKRW
	}
	applyUSDCashRate(assets, usdKRW)

	respondJSON(w, http.StatusOK, assets)
}

func (h *Handler) createAsset(w http.ResponseWriter, r *http.Request) {
	var req models.CreateOtherAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}

	// ── 필수 필드 검증 ────────────────────────────────────────────────────────
	var errs []string
	if req.UserID == "" {
		errs = append(errs, "user_id is required")
	}
	if req.AssetType == "" {
		errs = append(errs, "asset_type is required (부동산|예/적금|현금|대출|기타)")
	}
	if req.Name == "" {
		errs = append(errs, "name is required")
	}
	if len(errs) > 0 {
		respondJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "validation failed",
			"fields": errs,
		})
		return
	}

	now := time.Now().UTC()
	acquiredAt := now
	if req.AcquiredAt != nil {
		acquiredAt = *req.AcquiredAt
	}
	currency := strings.ToUpper(strings.TrimSpace(req.Currency))
	if currency == "" {
		currency = "KRW"
	}

	// IsLiability 자동 결정 — 대출만 부채
	isLiability := req.AssetType == models.AssetTypeLoan
	isLocked := req.IsLocked
	if isLiability {
		isLocked = false
	}

	// USD 현금: 환율 적용해 ValueKRW 계산
	valueKRW := req.ValueKRW
	if req.AssetType == models.AssetTypeCash && currency == "USD" && req.ValueUSD != nil {
		usdKRW, fxErr := h.priceSvc.FetchUSDKRW(r.Context())
		if fxErr != nil || usdKRW == 0 {
			usdKRW = service.FallbackUSDKRW
		}
		valueKRW = int64(*req.ValueUSD * usdKRW)
	}

	asset := &models.OtherAsset{
		CoupleID:     h.coupleID,
		UserID:       req.UserID,
		AssetType:    req.AssetType,
		Name:         req.Name,
		Description:  req.Description,
		ValueKRW:     valueKRW,
		ValueUSD:     req.ValueUSD,
		CostKRW:      req.CostKRW,
		Currency:     currency,
		IsLiability:  isLiability,
		IsLocked:     isLocked,
		Location:     req.Location,
		MaturityDate: req.MaturityDate,
		InterestRate: req.InterestRate,
		LoanType:     req.LoanType,
		PaymentDay:   req.PaymentDay,
		Memo:         req.Memo,
		AcquiredAt:   acquiredAt,
	}

	created, err := h.assetRepo.Create(r.Context(), asset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	// 대출 자산 등록 시 고정비 자동 생성
	if created.AssetType == models.AssetTypeLoan && created.PaymentDay > 0 {
		monthly := calcLoanMonthlyPayment(created.ValueKRW, created.InterestRate, created.LoanType, created.MaturityDate)
		if monthly > 0 {
			owner := models.FixedOwnerJoint
			if u, err := h.userRepo.GetUser(r.Context(), created.UserID); err == nil {
				switch u.Role {
				case "husband":
					owner = models.FixedOwnerHusband
				case "wife":
					owner = models.FixedOwnerWife
				}
			}
			fe := &models.FixedExpense{
				CoupleID:   h.coupleID,
				UserID:     created.UserID,
				Owner:      owner,
				Kind:       models.FixedExpenseKindSpending,
				Title:      created.Name + " 납입금",
				Category:   "대출상환",
				Amount:     monthly,
				Currency:   "KRW",
				Cycle:      models.RecurringMonthly,
				DayOfMonth: created.PaymentDay,
				Memo:       "대출 자산 등록 시 자동 생성",
			}
			_, _ = h.feRepo.Create(r.Context(), fe)
		}
	}

	respondJSON(w, http.StatusCreated, created)
}

func (h *Handler) getAsset(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	asset, err := h.assetRepo.GetByID(ctx, id)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	if asset.AssetType == models.AssetTypeCash && strings.ToUpper(asset.Currency) == "USD" && asset.ValueUSD != nil {
		usdKRW, fxErr := h.priceSvc.FetchUSDKRW(ctx)
		if fxErr != nil || usdKRW == 0 {
			usdKRW = service.FallbackUSDKRW
		}
		asset.ValueKRW = int64(*asset.ValueUSD * usdKRW)
	}
	respondJSON(w, http.StatusOK, asset)
}

// updateAsset performs a partial update — only non-nil pointer fields overwrite.
func (h *Handler) updateAsset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	ctx := r.Context()

	existing, err := h.assetRepo.GetByID(ctx, id)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}

	var req models.UpdateOtherAssetRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}

	// Apply partial update
	if req.UserID != nil {
		existing.UserID = *req.UserID
	}
	if req.AssetType != nil {
		existing.AssetType = *req.AssetType
	}
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.ValueKRW != nil {
		existing.ValueKRW = *req.ValueKRW
	}
	if req.ValueUSD != nil {
		existing.ValueUSD = req.ValueUSD
	}
	if req.CostKRW != nil {
		existing.CostKRW = *req.CostKRW
	}
	if req.IsLocked != nil {
		existing.IsLocked = *req.IsLocked
	}
	if req.Location != nil {
		existing.Location = req.Location
	}
	if req.MaturityDate != nil {
		existing.MaturityDate = req.MaturityDate
	}
	if req.InterestRate != nil {
		existing.InterestRate = req.InterestRate
	}
	if req.LoanType != nil {
		existing.LoanType = *req.LoanType
	}
	if req.PaymentDay != nil {
		existing.PaymentDay = *req.PaymentDay
	}
	if req.Memo != nil {
		existing.Memo = *req.Memo
	}

	// IsLiability 항상 타입으로 결정
	existing.IsLiability = existing.AssetType == models.AssetTypeLoan
	if existing.IsLiability {
		existing.IsLocked = false
	}

	// USD 현금: ValueKRW 재계산
	if existing.AssetType == models.AssetTypeCash && strings.ToUpper(existing.Currency) == "USD" && existing.ValueUSD != nil {
		usdKRW, fxErr := h.priceSvc.FetchUSDKRW(ctx)
		if fxErr != nil || usdKRW == 0 {
			usdKRW = service.FallbackUSDKRW
		}
		existing.ValueKRW = int64(*existing.ValueUSD * usdKRW)
	}

	updated, err := h.assetRepo.Update(ctx, existing)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

func (h *Handler) deleteAsset(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.assetRepo.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// createLoanFixedExpense creates a monthly fixed expense for an existing loan asset.
// POST /api/assets/{id}/loan-expense
func (h *Handler) createLoanFixedExpense(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	asset, err := h.assetRepo.GetByID(ctx, id)
	if err != nil {
		respondError(w, http.StatusNotFound, fmt.Errorf("asset not found: %w", err))
		return
	}
	if asset.AssetType != models.AssetTypeLoan {
		respondError(w, http.StatusBadRequest, fmt.Errorf("asset is not a loan"))
		return
	}
	if asset.PaymentDay == 0 {
		respondError(w, http.StatusBadRequest, fmt.Errorf("loan has no payment_day set"))
		return
	}

	monthly := calcLoanMonthlyPayment(asset.ValueKRW, asset.InterestRate, asset.LoanType, asset.MaturityDate)
	if monthly == 0 {
		respondError(w, http.StatusBadRequest, fmt.Errorf("cannot calculate monthly payment — check interest_rate and maturity_date"))
		return
	}

	// 중복 방지: 동일 제목 고정비 이미 존재하면 409
	expectedTitle := asset.Name + " 납입금"
	existingFEs, _ := h.feRepo.ListByCouple(ctx, h.coupleID)
	for _, fe := range existingFEs {
		if fe.Title == expectedTitle {
			respondError(w, http.StatusConflict, fmt.Errorf("fixed expense already exists: %q", expectedTitle))
			return
		}
	}

	owner := models.FixedOwnerJoint
	if u, err := h.userRepo.GetUser(ctx, asset.UserID); err == nil {
		switch u.Role {
		case "husband":
			owner = models.FixedOwnerHusband
		case "wife":
			owner = models.FixedOwnerWife
		}
	}

	fe := &models.FixedExpense{
		CoupleID:   h.coupleID,
		UserID:     asset.UserID,
		Owner:      owner,
		Kind:       models.FixedExpenseKindSpending,
		Title:      asset.Name + " 납입금",
		Category:   "대출상환",
		Amount:     monthly,
		Currency:   "KRW",
		Cycle:      models.RecurringMonthly,
		DayOfMonth: asset.PaymentDay,
		Memo:       "대출 자산에서 자동 생성",
	}
	created, err := h.feRepo.Create(ctx, fe)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusCreated, created)
}

// netWorth aggregates stock portfolio + other assets into a single KRW summary.
// GET /api/assets/net-worth
func (h *Handler) netWorth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 주식 포트폴리오 합산 (USD/KRW 환율 적용)
	usdKRW, fxErr := h.priceSvc.FetchUSDKRW(ctx)
	if fxErr != nil || usdKRW == 0 {
		usdKRW = service.FallbackUSDKRW
	}

	// 기타 자산 합산
	otherAssets, err := h.assetRepo.ListByCouple(ctx, h.coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	applyUSDCashRate(otherAssets, usdKRW)
	var assetValueKRW, liabilityKRW int64
	for _, a := range otherAssets {
		if a.IsLiability {
			liabilityKRW += a.ValueKRW
		} else {
			assetValueKRW += a.ValueKRW
		}
	}

	stocks, _ := h.stockRepo.ListByCouple(ctx, h.coupleID)
	var stockValueKRW float64
	for _, s := range stocks {
		snap, _ := h.stockRepo.GetPriceSnapshot(ctx, s.Symbol)
		if snap == nil {
			continue
		}
		val := snap.Price * s.Quantity
		if strings.ToUpper(s.Currency) == "USD" {
			val *= usdKRW
		}
		stockValueKRW += val
	}

	summary := models.NetWorthSummary{
		StockValueKRW: stockValueKRW,
		AssetValueKRW: assetValueKRW,
		LiabilityKRW:  liabilityKRW,
		NetWorthKRW:   stockValueKRW + float64(assetValueKRW) - float64(liabilityKRW),
		CalculatedAt:  time.Now().UTC(),
	}
	respondJSON(w, http.StatusOK, summary)
}

// ─────────────────────────────────────────────
//  Dividend handlers (배당)
// ─────────────────────────────────────────────

func (h *Handler) listDividends(w http.ResponseWriter, r *http.Request) {
	divs, err := h.divRepo.ListByCouple(r.Context(), h.coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, divs)
}

// createDividend: POST /api/dividends
// Computes derived fields (total_amount, after_tax_amount, amount_krw) server-side
// so they are always consistent and stored for later ledger creation.
func (h *Handler) createDividend(w http.ResponseWriter, r *http.Request) {
	var req models.CreateDividendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}
	if req.Symbol == "" || req.AmountPerShare <= 0 || req.Quantity <= 0 {
		respondError(w, http.StatusBadRequest, fmt.Errorf("symbol, quantity, and amount_per_share are required"))
		return
	}
	if req.Currency == "" {
		req.Currency = "USD"
	}
	if req.TaxRate <= 0 {
		req.TaxRate = 0.154 // US 배당소득세 기본값 15.4%
	}

	total := req.Quantity * req.AmountPerShare
	afterTax := total * (1 - req.TaxRate)
	var amountKRW int64
	if strings.ToUpper(req.Currency) == "USD" && req.USDKRWRate > 0 {
		amountKRW = int64(afterTax * req.USDKRWRate)
	} else {
		amountKRW = int64(afterTax)
	}

	d := &models.DividendEvent{
		CoupleID:       h.coupleID,
		UserID:         req.UserID,
		StockAssetID:   req.StockAssetID,
		Symbol:         req.Symbol,
		Exchange:       req.Exchange,
		Name:           req.Name,
		Quantity:       req.Quantity,
		AmountPerShare: req.AmountPerShare,
		Currency:       req.Currency,
		TotalAmount:    total,
		TaxRate:        req.TaxRate,
		AfterTaxAmount: afterTax,
		USDKRWRate:     req.USDKRWRate,
		AmountKRW:      amountKRW,
		ExDividendDate: req.ExDividendDate,
		PaymentDate:    req.PaymentDate,
		Memo:           req.Memo,
	}
	created, err := h.divRepo.Create(r.Context(), d)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	// Auto-apply: create ledger income transaction immediately
	if applyErr := h.applyDividendToLedger(r.Context(), created); applyErr != nil {
		// Log but don't fail — dividend is saved, ledger entry can be retried
		_ = applyErr
	}

	respondJSON(w, http.StatusCreated, created)
}

func (h *Handler) deleteDividend(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.divRepo.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// applyDividendToLedger creates an income transaction and marks the dividend applied.
func (h *Handler) applyDividendToLedger(ctx context.Context, d *models.DividendEvent) error {
	title := fmt.Sprintf("%s 배당금", d.Name)
	if d.Symbol != "" {
		title = fmt.Sprintf("%s (%s) 배당금", d.Name, d.Symbol)
	}
	memo := fmt.Sprintf("주당 %.4f %s × %.2f주, 세후 %.4f %s (세율 %.1f%%)",
		d.AmountPerShare, d.Currency, d.Quantity,
		d.AfterTaxAmount, d.Currency, d.TaxRate*100)

	tx := &models.Transaction{
		CoupleID:      h.coupleID,
		UserID:        d.UserID,
		Type:          "income",
		Amount:        d.AmountKRW,
		Currency:      "KRW",
		Category:      "배당수입",
		Title:         title,
		Memo:          memo,
		Date:          d.PaymentDate,
		PaymentMethod: "해외주식 배당",
		IsFixed:       false,
		Tags:          []string{"배당", d.Symbol},
	}
	if _, err := h.txRepo.Create(ctx, tx); err != nil {
		return err
	}
	return h.divRepo.MarkApplied(ctx, d.ID)
}

// applyDividend: POST /api/dividends/{id}/apply (kept for backward compatibility)
func (h *Handler) applyDividend(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	d, err := h.divRepo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	if d.IsApplied {
		respondError(w, http.StatusConflict, fmt.Errorf("already applied to ledger"))
		return
	}
	if err := h.applyDividendToLedger(r.Context(), d); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": "applied"})
}

// dividendSummary: GET /api/dividends/summary?year=2026&month=6
func (h *Handler) dividendSummary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	year := time.Now().Year()
	if y := q.Get("year"); y != "" {
		if v, err := strconv.Atoi(y); err == nil {
			year = v
		}
	}

	events, err := h.divRepo.ListByYear(r.Context(), h.coupleID, year)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	if events == nil {
		events = make([]models.DividendEvent, 0)
	}

	// Filter by month if provided.
	if m := q.Get("month"); m != "" {
		if month, err := strconv.Atoi(m); err == nil {
			filtered := events[:0]
			for _, e := range events {
				if int(e.PaymentDate.Month()) == month {
					filtered = append(filtered, e)
				}
			}
			events = filtered
		}
	}

	var totalUSD, totalAfterTaxUSD float64
	var totalKRW int64
	applied, pending := 0, 0

	for _, d := range events {
		totalUSD += d.TotalAmount
		totalAfterTaxUSD += d.AfterTaxAmount
		totalKRW += d.AmountKRW
		if d.IsApplied {
			applied++
		} else {
			pending++
		}
	}

	respondJSON(w, http.StatusOK, models.DividendYearlySummary{
		Year:             year,
		TotalUSD:         totalUSD,
		TotalAfterTaxUSD: totalAfterTaxUSD,
		TotalKRW:         totalKRW,
		AppliedCount:     applied,
		PendingCount:     pending,
		Events:           events,
	})
}

// ─────────────────────────────────────────────
//  Fixed Expense handlers (고정비)
// ─────────────────────────────────────────────

func (h *Handler) listFixedExpenses(w http.ResponseWriter, r *http.Request) {
	fes, err := h.feRepo.ListByCouple(r.Context(), h.coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, fes)
}

func (h *Handler) createFixedExpense(w http.ResponseWriter, r *http.Request) {
	var req models.CreateFixedExpenseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}
	if req.Title == "" || req.Amount <= 0 || req.DayOfMonth < 1 || req.DayOfMonth > 28 {
		respondError(w, http.StatusBadRequest, fmt.Errorf("title, amount, and day_of_month(1-28) are required"))
		return
	}
	if req.Currency == "" {
		req.Currency = "KRW"
	}
	if req.Cycle == "" {
		req.Cycle = models.RecurringMonthly
	}
	kind := req.Kind
	if kind == "" {
		kind = models.FixedExpenseKindSpending
	}
	if kind == models.FixedExpenseKindSaving && req.SavingLink == nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("saving_link is required when kind=saving"))
		return
	}
	fe := &models.FixedExpense{
		CoupleID:   h.coupleID,
		UserID:     req.UserID,
		Owner:      req.Owner,
		Kind:       kind,
		Title:      req.Title,
		Category:   req.Category,
		Amount:     req.Amount,
		Currency:   req.Currency,
		Cycle:      req.Cycle,
		DayOfMonth: req.DayOfMonth,
		DayOfWeek:  req.DayOfWeek,
		Memo:       req.Memo,
		SavingLink: req.SavingLink,
	}
	created, err := h.feRepo.Create(r.Context(), fe)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusCreated, created)
}

func (h *Handler) updateFixedExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.feRepo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	var req models.UpdateFixedExpenseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}
	if req.Owner != nil      { existing.Owner = *req.Owner }
	if req.Kind != nil       { existing.Kind = *req.Kind }
	if req.Title != nil      { existing.Title = *req.Title }
	if req.Category != nil   { existing.Category = *req.Category }
	if req.Amount != nil     { existing.Amount = *req.Amount }
	if req.DayOfMonth != nil { existing.DayOfMonth = *req.DayOfMonth }
	if req.IsActive != nil {
		wasActive := existing.IsActive
		existing.IsActive = *req.IsActive
		if wasActive && !*req.IsActive {
			now := time.Now().UTC()
			existing.DeactivatedAt = &now
		} else if !wasActive && *req.IsActive {
			existing.DeactivatedAt = nil
		}
	}
	if req.Memo != nil       { existing.Memo = *req.Memo }
	if req.SavingLink != nil { existing.SavingLink = req.SavingLink }

	updated, err := h.feRepo.Update(r.Context(), existing)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

func (h *Handler) deleteFixedExpense(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.feRepo.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fixedExpenseSummary returns this month's total + applied/unapplied breakdown.
// Query params: year, month (default: current month).
func (h *Handler) fixedExpenseSummary(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	now := time.Now()
	year := now.Year()
	month := int(now.Month())
	if y := q.Get("year"); y != "" {
		if v, err := strconv.Atoi(y); err == nil { year = v }
	}
	if m := q.Get("month"); m != "" {
		if v, err := strconv.Atoi(m); err == nil { month = v }
	}

	fes, err := h.feRepo.ListByCouple(r.Context(), h.coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	txns, err := h.txRepo.ListByMonth(r.Context(), h.coupleID, year, month)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	// Build set of fixed-expense IDs already applied this month.
	applied := make(map[string]bool)
	for _, tx := range txns {
		if tx.FixedExpenseID != nil && *tx.FixedExpenseID != "" {
			applied[*tx.FixedExpenseID] = true
		}
	}

	var totalAmount int64
	var unapplied []models.FixedExpense
	appliedCount := 0

	startOfMonth := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	endOfMonth := time.Date(year, time.Month(month+1), 1, 0, 0, 0, 0, time.UTC)
	for _, fe := range fes {
		// Exclude items created after this month ended.
		if !fe.CreatedAt.Before(endOfMonth) {
			continue
		}
		// Exclude inactive items with no deactivation date (legacy data).
		if !fe.IsActive && fe.DeactivatedAt == nil {
			continue
		}
		// Exclude items deactivated before this month started.
		if fe.DeactivatedAt != nil && fe.DeactivatedAt.Before(startOfMonth) {
			continue
		}
		totalAmount += fe.Amount
		if applied[fe.ID] {
			appliedCount++
		} else {
			unapplied = append(unapplied, fe)
		}
	}
	if unapplied == nil {
		unapplied = make([]models.FixedExpense, 0)
	}

	respondJSON(w, http.StatusOK, models.FixedExpenseSummary{
		TotalAmount:  totalAmount,
		AppliedCount: appliedCount,
		TotalCount:   appliedCount + len(unapplied),
		Unapplied:    unapplied,
	})
}

// applyFixedExpenses creates Transaction records for unapplied fixed expenses.
// Already-applied ones (linked via fixed_expense_id) are skipped.
// Query params: year, month (default: current month).
func (h *Handler) applyFixedExpenses(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	now := time.Now()
	year := now.Year()
	month := int(now.Month())
	if y := q.Get("year"); y != "" {
		if v, err := strconv.Atoi(y); err == nil { year = v }
	}
	if m := q.Get("month"); m != "" {
		if v, err := strconv.Atoi(m); err == nil { month = v }
	}

	fes, err := h.feRepo.ListByCouple(r.Context(), h.coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	txns, err := h.txRepo.ListByMonth(r.Context(), h.coupleID, year, month)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	// Build role → userID map so owner (husband/wife) maps to actual user.
	roleToUserID := make(map[string]string)
	if users, uErr := h.userRepo.ListUsers(r.Context()); uErr == nil {
		for _, u := range users {
			roleToUserID[u.Role] = u.ID
		}
	}
	resolveOwner := func(owner models.FixedExpenseOwner, fallback string) string {
		if owner == models.FixedOwnerJoint {
			return fallback
		}
		if id, ok := roleToUserID[string(owner)]; ok {
			return id
		}
		return fallback
	}

	// Build set of already-applied IDs.
	applied := make(map[string]bool)
	for _, tx := range txns {
		if tx.FixedExpenseID != nil && *tx.FixedExpenseID != "" {
			applied[*tx.FixedExpenseID] = true
		}
	}

	var result []models.Transaction
	skipped := 0

	for _, fe := range fes {
		if !fe.IsActive {
			continue
		}
		if applied[fe.ID] {
			skipped++
			continue
		}

		// Clamp day to valid range for the month.
		day := fe.DayOfMonth
		if day > 28 {
			day = 28
		}
		txDate := time.Date(year, time.Month(month), day, 0, 0, 0, 0, time.UTC)
		feID := fe.ID

		// Saving fixed expenses go through SavingService for atomic asset update.
		if fe.Kind == models.FixedExpenseKindSaving && fe.SavingLink != nil {
			created, err := h.savingSvc.ApplySaving(r.Context(), service.ApplySavingRequest{
				UserID:         resolveOwner(fe.Owner, fe.UserID),
				AmountKRW:      fe.Amount,
				Title:          fe.Title,
				Memo:           fe.Memo,
				Date:           txDate,
				PaymentMethod:  "자동이체",
				IsFixed:        true,
				FixedExpenseID: &feID,
				Tags:           []string{"고정비", "저축"},
				Link:           *fe.SavingLink,
			})
			if err != nil {
				respondError(w, http.StatusInternalServerError, err)
				return
			}
			result = append(result, *created)
			continue
		}

		tx := &models.Transaction{
			CoupleID:       h.coupleID,
			UserID:         resolveOwner(fe.Owner, fe.UserID),
			Type:           "expense",
			Amount:         fe.Amount,
			Currency:       fe.Currency,
			Category:       fe.Category,
			Title:          fe.Title,
			Memo:           fe.Memo,
			Date:           txDate,
			PaymentMethod:  "자동이체",
			IsFixed:        true,
			Tags:           []string{"고정비"},
			FixedExpenseID: &feID,
		}
		if tx.Tags == nil {
			tx.Tags = make([]string, 0)
		}

		created, err := h.txRepo.Create(r.Context(), tx)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
		result = append(result, *created)
	}

	if result == nil {
		result = make([]models.Transaction, 0)
	}

	respondJSON(w, http.StatusOK, models.ApplyFixedExpensesResult{
		Applied: result,
		Skipped: skipped,
		Total:   len(fes),
	})
}

// ─────────────────────────────────────────────
//  User handlers
// ─────────────────────────────────────────────

func (h *Handler) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.userRepo.ListUsers(context.Background())
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, users)
}

func (h *Handler) getCouple(w http.ResponseWriter, r *http.Request) {
	couple, err := h.userRepo.GetCouple(r.Context(), h.coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, couple)
}

func (h *Handler) updateCouple(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MonthlyBudget  int64 `json:"monthly_budget"`
		LedgerStartDay int   `json:"ledger_start_day"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}
	// If ledger_start_day is not provided (zero value), keep existing value.
	if req.LedgerStartDay == 0 {
		existing, err := h.userRepo.GetCouple(r.Context(), h.coupleID)
		if err == nil {
			req.LedgerStartDay = existing.LedgerStartDay
		}
		if req.LedgerStartDay == 0 {
			req.LedgerStartDay = 1 // fallback default
		}
	}
	couple, err := h.userRepo.UpdateCouple(r.Context(), h.coupleID, req.MonthlyBudget, req.LedgerStartDay)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, couple)
}

// ─────────────────────────────────────────────
//  Helpers
// ─────────────────────────────────────────────

func respondJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func respondError(w http.ResponseWriter, status int, err error) {
	respondJSON(w, status, map[string]string{"error": err.Error()})
}

// ─────────────────────────────────────────────
//  Schedule handlers
// ─────────────────────────────────────────────

func (h *Handler) listSchedules(w http.ResponseWriter, r *http.Request) {
	yearStr := r.URL.Query().Get("year")
	monthStr := r.URL.Query().Get("month")

	var schedules []models.Schedule
	var err error

	if yearStr != "" && monthStr != "" {
		year, _ := strconv.Atoi(yearStr)
		month, _ := strconv.Atoi(monthStr)
		schedules, err = h.scheduleRepo.ListByMonth(r.Context(), h.coupleID, year, month)
	} else {
		schedules, err = h.scheduleRepo.ListByCouple(r.Context(), h.coupleID)
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, schedules)
}

func (h *Handler) createSchedule(w http.ResponseWriter, r *http.Request) {
	var req models.CreateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	startDate, err := time.Parse("2006-01-02", req.StartDate)
	if err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid start_date: %w", err))
		return
	}
	s := &models.Schedule{
		CoupleID:    h.coupleID,
		UserID:      req.UserID,
		Title:       req.Title,
		Description: req.Description,
		StartDate:   startDate,
		AllDay:      req.AllDay,
		IsDDay:      req.IsDDay,
		DDayLabel:   req.DDayLabel,
		Color:       req.Color,
	}
	if req.EndDate != nil {
		endDate, err := time.Parse("2006-01-02", *req.EndDate)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Errorf("invalid end_date: %w", err))
			return
		}
		s.EndDate = &endDate
	}
	if s.Color == "" {
		s.Color = "indigo"
	}
	created, err := h.scheduleRepo.Create(r.Context(), s)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusCreated, created)
}

func (h *Handler) updateSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.scheduleRepo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	var req models.UpdateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.Title != nil       { existing.Title = *req.Title }
	if req.Description != nil { existing.Description = *req.Description }
	if req.AllDay != nil      { existing.AllDay = *req.AllDay }
	if req.IsDDay != nil      { existing.IsDDay = *req.IsDDay }
	if req.DDayLabel != nil   { existing.DDayLabel = *req.DDayLabel }
	if req.Color != nil       { existing.Color = *req.Color }
	if req.StartDate != nil {
		d, err := time.Parse("2006-01-02", *req.StartDate)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Errorf("invalid start_date: %w", err))
			return
		}
		existing.StartDate = d
	}
	if req.EndDate != nil {
		d, err := time.Parse("2006-01-02", *req.EndDate)
		if err != nil {
			respondError(w, http.StatusBadRequest, fmt.Errorf("invalid end_date: %w", err))
			return
		}
		existing.EndDate = &d
	}
	updated, err := h.scheduleRepo.Update(r.Context(), existing)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

func (h *Handler) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.scheduleRepo.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ─────────────────────────────────────────────
//  Diary handlers
// ─────────────────────────────────────────────

func (h *Handler) listDiaries(w http.ResponseWriter, r *http.Request) {
	yearStr := r.URL.Query().Get("year")
	monthStr := r.URL.Query().Get("month")

	var diaries []models.DiaryEntry
	var err error

	if yearStr != "" && monthStr != "" {
		year, _ := strconv.Atoi(yearStr)
		month, _ := strconv.Atoi(monthStr)
		diaries, err = h.diaryRepo.ListByMonth(r.Context(), h.coupleID, year, month)
	} else {
		diaries, err = h.diaryRepo.ListByCouple(r.Context(), h.coupleID)
	}
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, diaries)
}

func (h *Handler) createDiary(w http.ResponseWriter, r *http.Request) {
	var req models.CreateDiaryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.Date == "" {
		req.Date = time.Now().Format("2006-01-02")
	}
	d := &models.DiaryEntry{
		CoupleID: h.coupleID,
		UserID:   req.UserID,
		Date:     req.Date,
		Content:  req.Content,
		Mood:     req.Mood,
		Photos:   []string{},
	}
	created, err := h.diaryRepo.Create(r.Context(), d)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusCreated, created)
}

func (h *Handler) updateDiary(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.diaryRepo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	var req models.UpdateDiaryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	if req.Content != nil { existing.Content = *req.Content }
	if req.Mood != nil    { existing.Mood = *req.Mood }
	updated, err := h.diaryRepo.Update(r.Context(), existing)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, updated)
}

func (h *Handler) deleteDiary(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	entry, err := h.diaryRepo.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	for _, photoURL := range entry.Photos {
		if h.stor != nil {
			_ = h.stor.Delete(path.Base(photoURL))
		} else {
			_ = os.Remove(filepath.Join(h.uploadsDir, filepath.Base(photoURL)))
		}
	}
	if err := h.diaryRepo.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) uploadDiaryPhoto(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if _, err := h.diaryRepo.GetByID(r.Context(), id); err != nil {
		respondError(w, http.StatusNotFound, err)
		return
	}
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("file too large"))
		return
	}
	file, header, err := r.FormFile("photo")
	if err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("missing photo field"))
		return
	}
	defer file.Close()

	ext := filepath.Ext(header.Filename)
	filename := uuid.NewString() + ext

	data, err := io.ReadAll(file)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	var photoURL string
	if h.stor != nil {
		contentType := header.Header.Get("Content-Type")
		if contentType == "" {
			contentType = "image/jpeg"
		}
		publicURL, err := h.stor.Upload(filename, data, contentType)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
		photoURL = publicURL
	} else {
		dst := filepath.Join(h.uploadsDir, filename)
		if err := os.MkdirAll(h.uploadsDir, 0755); err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
		if err := os.WriteFile(dst, data, 0644); err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
		photoURL = "/uploads/" + filename
	}

	if err := h.diaryRepo.AddPhoto(r.Context(), id, photoURL); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusCreated, map[string]string{"filename": filename, "url": photoURL})
}

func (h *Handler) deleteDiaryPhoto(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	filename := chi.URLParam(r, "filename")

	var photoURL string
	if h.stor != nil {
		photoURL = h.stor.PublicURL(filename)
		if err := h.stor.Delete(filename); err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
	} else {
		photoURL = "/uploads/" + filename
		_ = os.Remove(filepath.Join(h.uploadsDir, filename))
	}

	if err := h.diaryRepo.DeletePhoto(r.Context(), id, photoURL); err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) serveUpload(w http.ResponseWriter, r *http.Request) {
	filename := chi.URLParam(r, "filename")
	filename = filepath.Base(filename)
	http.ServeFile(w, r, filepath.Join(h.uploadsDir, filename))
}

// getCategories — GET /api/categories
func (h *Handler) getCategories(w http.ResponseWriter, r *http.Request) {
	cats, err := h.catRepo.Get(r.Context(), h.coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, cats)
}

// updateCategories — PUT /api/categories
func (h *Handler) updateCategories(w http.ResponseWriter, r *http.Request) {
	var cats models.Categories
	if err := json.NewDecoder(r.Body).Decode(&cats); err != nil {
		respondError(w, http.StatusBadRequest, err)
		return
	}
	updated, err := h.catRepo.Update(r.Context(), h.coupleID, &cats)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, updated)
}
