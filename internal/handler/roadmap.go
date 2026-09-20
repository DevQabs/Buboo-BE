// roadmap.go — 목표 순자산 플랜 API.
//
// 목표와 가정이 아직 없는 부부에게도 화면이 그려지도록, 조회는 저장된 값이
// 없으면 기본값을 돌려준다. 저장은 PUT 이 처음 불릴 때 일어난다.
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/yourname/couple-app/internal/auth"
	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/service"
)

// 목표가 없을 때 쓰는 기본값. 2032년 12월, 부부 둘 다 만 40세에 순자산 10억.
var defaultGoal = models.RoadmapGoal{
	Title:      "10억 플랜",
	TargetKRW:  1_000_000_000,
	TargetDate: time.Date(2032, time.December, 31, 0, 0, 0, 0, time.UTC),
	BirthYear:  1992,
	IsActive:   true,
}

// 가정이 없을 때 쓰는 기본값. 배당 계획은 비워 둔다 — 종목별 주당 배당은
// 사용자가 화면에서 채워야 하는 값이고, 없는 값을 지어내면 궤적이 틀어진다.
var defaultAssumptions = models.RoadmapAssumptions{
	DividendTaxRate: 0.154,
	Contributions:   []models.Contribution{},
	DividendPlan:    []models.DividendHolding{},
}

// goalOrDefault는 저장된 목표를, 없으면 기본 목표를 준다.
func (h *Handler) goalOrDefault(r *http.Request, coupleID string) (*models.RoadmapGoal, error) {
	goal, err := h.roadmapRepo.ActiveGoal(r.Context(), coupleID)
	if err != nil {
		return nil, err
	}
	if goal == nil {
		g := defaultGoal
		g.CoupleID = coupleID
		return &g, nil
	}
	return goal, nil
}

// assumptionsOrDefault는 목표에 딸린 가정을, 없으면 기본 가정을 준다.
// 기타 자산은 저장값이 없으면 실제 보유 자산에서 채운다.
func (h *Handler) assumptionsOrDefault(r *http.Request, coupleID, goalID string) (*models.RoadmapAssumptions, error) {
	if goalID != "" {
		a, err := h.roadmapRepo.Assumptions(r.Context(), goalID)
		if err != nil {
			return nil, err
		}
		if a != nil {
			return a, nil
		}
	}
	a := defaultAssumptions
	a.CoupleID = coupleID
	a.GoalID = goalID
	return &a, nil
}

// getRoadmapGoal — GET /api/roadmap/goal
func (h *Handler) getRoadmapGoal(w http.ResponseWriter, r *http.Request) {
	goal, err := h.goalOrDefault(r, auth.CoupleIDFromCtx(r.Context()))
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, goal)
}

// putRoadmapGoal — PUT /api/roadmap/goal
func (h *Handler) putRoadmapGoal(w http.ResponseWriter, r *http.Request) {
	var req models.RoadmapGoal
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}
	if req.TargetKRW <= 0 || req.TargetDate.IsZero() {
		respondError(w, http.StatusBadRequest, fmt.Errorf("목표 금액과 목표일이 필요합니다"))
		return
	}

	coupleID := auth.CoupleIDFromCtx(r.Context())
	existing, err := h.roadmapRepo.ActiveGoal(r.Context(), coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	req.CoupleID = coupleID
	req.IsActive = true
	if existing != nil {
		req.ID = existing.ID // 목표는 부부당 하나. 새로 만들지 않고 덮어쓴다
	} else {
		req.ID = uuid.NewString()
	}

	saved, err := h.roadmapRepo.UpsertGoal(r.Context(), &req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, saved)
}

// getRoadmapAssumptions — GET /api/roadmap/assumptions
func (h *Handler) getRoadmapAssumptions(w http.ResponseWriter, r *http.Request) {
	coupleID := auth.CoupleIDFromCtx(r.Context())
	goal, err := h.goalOrDefault(r, coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	a, err := h.assumptionsOrDefault(r, coupleID, goal.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, a)
}

// putRoadmapAssumptions — PUT /api/roadmap/assumptions
//
// 목표가 아직 저장되지 않았으면 기본 목표를 먼저 만든다. 가정은 목표에
// 딸리므로 goal_id 없이 저장할 수 없다.
func (h *Handler) putRoadmapAssumptions(w http.ResponseWriter, r *http.Request) {
	var req models.RoadmapAssumptions
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, fmt.Errorf("invalid body: %w", err))
		return
	}

	coupleID := auth.CoupleIDFromCtx(r.Context())
	goal, err := h.ensureGoal(r, coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	existing, err := h.roadmapRepo.Assumptions(r.Context(), goal.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	req.CoupleID = coupleID
	req.GoalID = goal.ID
	if existing != nil {
		req.ID = existing.ID
	} else {
		req.ID = uuid.NewString()
	}
	if req.Contributions == nil {
		req.Contributions = []models.Contribution{}
	}
	if req.DividendPlan == nil {
		req.DividendPlan = []models.DividendHolding{}
	}

	saved, err := h.roadmapRepo.UpsertAssumptions(r.Context(), &req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, saved)
}

// ensureGoal은 저장된 목표를 주고, 없으면 기본 목표를 저장해서 준다.
func (h *Handler) ensureGoal(r *http.Request, coupleID string) (*models.RoadmapGoal, error) {
	goal, err := h.roadmapRepo.ActiveGoal(r.Context(), coupleID)
	if err != nil {
		return nil, err
	}
	if goal != nil {
		return goal, nil
	}
	g := defaultGoal
	g.ID = uuid.NewString()
	g.CoupleID = coupleID
	return h.roadmapRepo.UpsertGoal(r.Context(), &g)
}

// postNetWorthSnapshot — POST /api/roadmap/snapshot
//
// 이번 달 순자산을 적재한다. 값은 wealth 탭이 쓰는 것과 같은 경로로 구한다.
// 별도 계산 경로를 만들지 않는다.
func (h *Handler) postNetWorthSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	coupleID := auth.CoupleIDFromCtx(ctx)

	stockKRW, assetKRW, liabilityKRW, err := h.netWorthParts(r)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}

	now := time.Now()
	snap := &models.NetWorthSnapshot{
		ID:            uuid.NewString(),
		CoupleID:      coupleID,
		SnapshotMonth: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC),
		StockKRW:      stockKRW,
		AssetKRW:      assetKRW,
		LiabilityKRW:  liabilityKRW,
		NetWorthKRW:   stockKRW + assetKRW - liabilityKRW,
	}
	saved, err := h.roadmapRepo.UpsertSnapshot(ctx, snap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, saved)
}

// netWorthParts는 현재 순자산을 주식/기타자산/부채로 나눠 낸다.
func (h *Handler) netWorthParts(r *http.Request) (stockKRW, assetKRW, liabilityKRW int64, err error) {
	ctx := r.Context()
	coupleID := auth.CoupleIDFromCtx(ctx)

	usdKRW, fxErr := h.priceSvc.FetchUSDKRW(ctx)
	if fxErr != nil || usdKRW == 0 {
		usdKRW = service.FallbackUSDKRW
	}

	// 기타 자산과 보유 주식은 서로 무관하다. 원격 DB 왕복이 200ms씩이라
	// 순서대로 기다릴 이유가 없다.
	var (
		otherAssets []models.OtherAsset
		stocks      []models.StockAsset
		assetErr    error
		stockErr    error
		wg          sync.WaitGroup
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		otherAssets, assetErr = h.assetRepo.ListByCouple(ctx, coupleID)
	}()
	go func() {
		defer wg.Done()
		stocks, stockErr = h.stockRepo.ListByCouple(ctx, coupleID)
	}()
	wg.Wait()
	if assetErr != nil {
		return 0, 0, 0, assetErr
	}
	if stockErr != nil {
		return 0, 0, 0, stockErr
	}

	applyUSDCashRate(otherAssets, usdKRW)
	for _, a := range otherAssets {
		if a.IsLiability {
			liabilityKRW += a.ValueKRW
		} else {
			assetKRW += a.ValueKRW
		}
	}
	symbols := make([]string, 0, len(stocks))
	for _, s := range stocks {
		symbols = append(symbols, s.Symbol)
	}
	snaps, err := h.stockRepo.ListPriceSnapshots(ctx, symbols)
	if err != nil {
		return 0, 0, 0, err
	}
	var stockValue float64
	for _, s := range stocks {
		snap, ok := snaps[s.Symbol]
		if !ok {
			continue
		}
		val := snap.Price * s.Quantity
		if strings.ToUpper(s.Currency) == "USD" {
			val *= usdKRW
		}
		stockValue += val
	}
	return int64(stockValue), assetKRW, liabilityKRW, nil
}

// dividendPlanFromHoldings는 지금 보유한 종목에서 배당 계획을 만든다.
//
// 저장된 계획을 쓰지 않는 이유는 금방 낡기 때문이다. 주식을 사고팔면 주식수가
// 달라지고 배당도 매년 인상된다. 종목별 최근 3개년 성장률과 TTM 배당을
// 야후에서 받아 매번 새로 만든다. 배당률이 낮은 종목은 빠진다.
func (h *Handler) dividendPlanFromHoldings(ctx context.Context, coupleID string) ([]models.DividendHolding, error) {
	stocks, err := h.stockRepo.ListByCouple(ctx, coupleID)
	if err != nil {
		return nil, err
	}

	// 같은 종목을 둘이 나눠 갖고 있으므로 주식수를 합친다.
	type holding struct {
		exchange string
		shares   float64
	}
	merged := make(map[string]holding, len(stocks))
	symbols := make([]string, 0, len(stocks))
	for _, s := range stocks {
		if _, ok := merged[s.Symbol]; !ok {
			symbols = append(symbols, s.Symbol)
		}
		m := merged[s.Symbol]
		m.exchange = s.Exchange
		m.shares += s.Quantity
		merged[s.Symbol] = m
	}

	snaps, err := h.stockRepo.ListPriceSnapshots(ctx, symbols)
	if err != nil {
		return nil, err
	}

	// 종목마다 HTTP 한 번이라 순서대로 기다리면 종목 수만큼 느려진다.
	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		plan = make([]models.DividendHolding, 0, len(symbols))
	)
	for _, sym := range symbols {
		wg.Add(1)
		go func(sym string) {
			defer wg.Done()
			hist, err := h.priceSvc.FetchDividendHistory(ctx, sym, merged[sym].exchange)
			if err != nil || hist == nil {
				return // 배당 없는 종목이거나 조회 실패. 계획에서 빠진다
			}
			snap, ok := snaps[sym]
			if !ok {
				return
			}
			d, include := service.DividendPlanFor(sym, merged[sym].shares, snap.Price, hist)
			if !include {
				return
			}
			mu.Lock()
			plan = append(plan, d)
			mu.Unlock()
		}(sym)
	}
	wg.Wait()

	sort.Slice(plan, func(i, j int) bool { return plan[i].Symbol < plan[j].Symbol })
	return plan, nil
}

// roadmapProjection — GET /api/roadmap/projection
//
// 계획(가정대로 굴린 궤적)과 실적(스냅샷)을 한 번에 준다. 필요 수익률은
// 목표일에 목표액이 되는 가격상승률을 역산한 값이다.
func (h *Handler) roadmapProjection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	coupleID := auth.CoupleIDFromCtx(ctx)

	// 세 갈래는 서로 독립이다. 순서대로 기다리면 원격 DB 왕복이 그대로 더해진다.
	var (
		goal                             *models.RoadmapGoal
		a                                *models.RoadmapAssumptions
		stockKRW, assetKRW, liabilityKRW int64
		snapshots                        []models.NetWorthSnapshot
		divPlan                          []models.DividendHolding
		goalErr, worthErr, snapErr       error
		divErr                           error
		wg                               sync.WaitGroup
	)
	wg.Add(4)
	go func() {
		defer wg.Done()
		// 가정은 목표에 딸려 있어 이 둘만 순서가 있다.
		if goal, goalErr = h.goalOrDefault(r, coupleID); goalErr == nil {
			a, goalErr = h.assumptionsOrDefault(r, coupleID, goal.ID)
		}
	}()
	go func() {
		defer wg.Done()
		stockKRW, assetKRW, liabilityKRW, worthErr = h.netWorthParts(r)
	}()
	go func() {
		defer wg.Done()
		snapshots, snapErr = h.roadmapRepo.ListSnapshots(ctx, coupleID)
	}()
	go func() {
		defer wg.Done()
		divPlan, divErr = h.dividendPlanFromHoldings(ctx, coupleID)
	}()
	wg.Wait()

	for _, err := range []error{goalErr, worthErr, snapErr, divErr} {
		if err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
	}
	// 배당 계획은 저장값이 아니라 지금 보유 종목에서 만든 것을 쓴다.
	a.DividendPlan = divPlan

	// 기타 자산은 저장된 가정이 있으면 그 값을, 없으면 실제 보유분을 쓴다.
	if a.OtherAssetsKRW == 0 {
		a.OtherAssetsKRW = assetKRW - liabilityKRW
	}
	netWorthKRW := stockKRW + a.OtherAssetsKRW

	usdKRW, fxErr := h.priceSvc.FetchUSDKRW(ctx)
	if fxErr != nil || usdKRW == 0 {
		usdKRW = service.FallbackUSDKRW
	}

	now := time.Now().UTC()
	growth := service.SolveRequiredGrowth(*a, stockKRW, usdKRW, goal.TargetKRW, now, goal.TargetDate, goal.BirthYear)
	a.PriceGrowth = growth
	years, months := service.Project(*a, stockKRW, usdKRW, now, goal.TargetDate, goal.BirthYear)

	// 실적은 그 해 마지막 스냅샷으로 채운다.
	actualByYear := make(map[int]int64, len(snapshots))
	for _, s := range snapshots {
		actualByYear[s.SnapshotMonth.Year()] = s.NetWorthKRW // 오래된 순이라 마지막이 남는다
	}
	// 올해는 스냅샷이 없어도 지금 순자산을 실적으로 본다. 그래야 화면에서
	// 계획선과 실적선이 첫날부터 두 줄로 보인다.
	if _, ok := actualByYear[now.Year()]; !ok {
		actualByYear[now.Year()] = netWorthKRW
	}
	for i := range years {
		if v, ok := actualByYear[years[i].Year]; ok {
			actual := v
			years[i].ActualNetWorthKRW = &actual
		}
	}

	// 궤적은 다음 달부터 시작한다. 차트가 허공에서 시작하지 않도록 이번 달을
	// 출발점으로 앞에 붙인다 — 계획과 실적이 같은 지점에서 갈라져 나간다.
	months = append([]models.RoadmapMonthPoint{{
		Month:                now.Format("2006-01"),
		ProjectedNetWorthKRW: netWorthKRW,
	}}, months...)

	// 월별 실적은 그 달 스냅샷으로 채운다. 이번 달은 스냅샷이 없어도 지금
	// 순자산을 쓴다.
	actualByMonth := make(map[string]int64, len(snapshots))
	for _, s := range snapshots {
		actualByMonth[s.SnapshotMonth.Format("2006-01")] = s.NetWorthKRW
	}
	if _, ok := actualByMonth[now.Format("2006-01")]; !ok {
		actualByMonth[now.Format("2006-01")] = netWorthKRW
	}
	for i := range months {
		if v, ok := actualByMonth[months[i].Month]; ok {
			actual := v
			months[i].ActualNetWorthKRW = &actual
		}
	}

	var out models.RoadmapProjection
	out.Goal.TargetKRW = goal.TargetKRW
	out.Goal.TargetDate = goal.TargetDate.Format("2006-01-02")
	out.Goal.AgeAtTarget = goal.TargetDate.Year() - goal.BirthYear
	out.Current.NetWorthKRW = netWorthKRW
	if goal.TargetKRW > 0 {
		out.Current.ProgressPct = float64(netWorthKRW) / float64(goal.TargetKRW) * 100
	}
	out.Current.DaysLeft = int(goal.TargetDate.Sub(now).Hours() / 24)
	out.RequiredPriceGrowth = growth
	out.CurrentDividendYield = service.DividendYield(*a, stockKRW, usdKRW, now.Year())
	out.RequiredTotalReturn = growth + out.CurrentDividendYield
	out.Years = years
	out.Months = months

	respondJSON(w, http.StatusOK, out)
}
