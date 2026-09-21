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
	// 목표가 바뀌면 옛 계획선은 의미가 없다. 다음 조회에서 새로 세운다.
	if err := h.roadmapRepo.DeleteBaseline(r.Context(), saved.ID); err != nil {
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
	// 배당 계획은 저장값이 아니라 지금 보유 종목에서 만든 것이다. 저장된 옛
	// 값을 돌려주면 화면이 계산과 다른 숫자를 보게 된다.
	market, err := h.loadMarket(r.Context(), coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	cands, err := h.dividendCandidates(r.Context(), market, a.DividendSymbols)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	a.DividendPlan = dividendPlanFrom(cands)
	respondJSON(w, http.StatusOK, map[string]any{
		"assumptions":         a,
		"dividend_candidates": cands,
	})
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
	// 배당 계획 자체는 저장하지 않는다. 보유 종목에서 매번 만들기 때문에,
	// 저장하면 낡은 값이 남아 어느 쪽이 진짜인지 헷갈리기만 한다. 어떤 종목을
	// 넣을지 고른 것만 저장한다.
	req.DividendPlan = []models.DividendHolding{}
	if req.DividendSymbols == nil {
		req.DividendSymbols = []string{}
	}

	saved, err := h.roadmapRepo.UpsertAssumptions(r.Context(), &req)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	// 가정을 바꾸면 계획을 다시 세운다. 다음 조회에서 이번 달 자산으로 출발한다.
	if err := h.roadmapRepo.DeleteBaseline(r.Context(), goal.ID); err != nil {
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

// newSnapshot은 이번 달 실적 한 건을 만든다. 같은 달은 덮어쓴다.
func newSnapshot(coupleID string, stockKRW, assetKRW, liabilityKRW int64, now time.Time) *models.NetWorthSnapshot {
	return &models.NetWorthSnapshot{
		ID:            uuid.NewString(),
		CoupleID:      coupleID,
		SnapshotMonth: time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC),
		StockKRW:      stockKRW,
		AssetKRW:      assetKRW,
		LiabilityKRW:  liabilityKRW,
		NetWorthKRW:   stockKRW + assetKRW - liabilityKRW,
	}
}

// postNetWorthSnapshot — POST /api/roadmap/snapshot
//
// 이번 달 순자산을 적재한다. 값은 wealth 탭이 쓰는 것과 같은 경로로 구한다.
// 별도 계산 경로를 만들지 않는다.
func (h *Handler) postNetWorthSnapshot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	coupleID := auth.CoupleIDFromCtx(ctx)

	market, err := h.loadMarket(ctx, coupleID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	stockKRW, assetKRW, liabilityKRW := market.netWorth()

	snap := newSnapshot(coupleID, stockKRW, assetKRW, liabilityKRW, time.Now())
	saved, err := h.roadmapRepo.UpsertSnapshot(ctx, snap)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	respondJSON(w, http.StatusOK, saved)
}

// marketView는 한 요청에서 쓰는 시장 데이터다.
//
// 순자산과 배당 계획 둘 다 같은 것(환율·보유 종목·시세)을 본다. 각자 읽으면
// 같은 요청 안에서 DB 왕복이 두 배가 되고, 두 계산이 서로 다른 시점의 값을
// 볼 수도 있다. 한 번 읽어 돌려쓴다.
type marketView struct {
	usdKRW      float64
	stocks      []models.StockAsset
	snaps       map[string]models.PriceSnapshot
	otherAssets []models.OtherAsset
}

// loadMarket은 환율·기타자산·보유주식을 한 번에 읽는다.
func (h *Handler) loadMarket(ctx context.Context, coupleID string) (*marketView, error) {
	m := &marketView{}

	// 셋 다 서로 무관하다. 원격 DB 왕복이 200ms씩이라 순서대로 기다릴 이유가 없다.
	var (
		assetErr, stockErr error
		wg                 sync.WaitGroup
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		usdKRW, err := h.priceSvc.FetchUSDKRW(ctx)
		if err != nil || usdKRW == 0 {
			usdKRW = service.FallbackUSDKRW
		}
		m.usdKRW = usdKRW
	}()
	go func() {
		defer wg.Done()
		m.otherAssets, assetErr = h.assetRepo.ListByCouple(ctx, coupleID)
	}()
	go func() {
		defer wg.Done()
		m.stocks, stockErr = h.stockRepo.ListByCouple(ctx, coupleID)
	}()
	wg.Wait()
	if assetErr != nil {
		return nil, assetErr
	}
	if stockErr != nil {
		return nil, stockErr
	}

	symbols := make([]string, 0, len(m.stocks))
	seen := make(map[string]bool, len(m.stocks))
	for _, s := range m.stocks {
		if !seen[s.Symbol] {
			seen[s.Symbol] = true
			symbols = append(symbols, s.Symbol)
		}
	}
	snaps, err := h.stockRepo.ListPriceSnapshots(ctx, symbols)
	if err != nil {
		return nil, err
	}
	m.snaps = snaps
	return m, nil
}

// netWorth는 순자산을 주식/기타자산/부채로 나눠 낸다.
func (m *marketView) netWorth() (stockKRW, assetKRW, liabilityKRW int64) {
	applyUSDCashRate(m.otherAssets, m.usdKRW)
	for _, a := range m.otherAssets {
		if a.IsLiability {
			liabilityKRW += a.ValueKRW
		} else {
			assetKRW += a.ValueKRW
		}
	}
	var stockValue float64
	for _, s := range m.stocks {
		snap, ok := m.snaps[s.Symbol]
		if !ok {
			continue
		}
		val := snap.Price * s.Quantity
		if strings.ToUpper(s.Currency) == "USD" {
			val *= m.usdKRW
		}
		stockValue += val
	}
	return int64(stockValue), assetKRW, liabilityKRW
}

// dividendCandidates는 보유 종목마다 배당 이력을 붙여 돌려준다.
//
// 저장된 계획을 쓰지 않는 이유는 금방 낡기 때문이다. 주식을 사고팔면 주식수가
// 달라지고 배당도 매년 인상된다. 주식수와 시세는 marketView 에서, 배당 이력은
// 야후에서 조회할 때마다 새로 읽는다.
func (h *Handler) dividendCandidates(ctx context.Context, m *marketView, selected []string) ([]models.DividendCandidate, error) {
	// 같은 종목을 둘이 나눠 갖고 있으므로 주식수를 합친다.
	type holding struct {
		exchange string
		shares   float64
	}
	merged := make(map[string]holding, len(m.stocks))
	symbols := make([]string, 0, len(m.stocks))
	for _, s := range m.stocks {
		if _, ok := merged[s.Symbol]; !ok {
			symbols = append(symbols, s.Symbol)
		}
		hold := merged[s.Symbol]
		hold.exchange = s.Exchange
		hold.shares += s.Quantity
		merged[s.Symbol] = hold
	}

	chosen := make(map[string]bool, len(selected))
	for _, sym := range selected {
		chosen[sym] = true
	}

	// 종목마다 HTTP 한 번이라 순서대로 기다리면 종목 수만큼 느려진다.
	var (
		mu  sync.Mutex
		wg  sync.WaitGroup
		out = make([]models.DividendCandidate, 0, len(symbols))
	)
	for _, sym := range symbols {
		wg.Add(1)
		go func(sym string) {
			defer wg.Done()
			hist, err := h.priceSvc.FetchDividendHistory(ctx, sym, merged[sym].exchange)
			if err != nil || hist == nil || hist.AnnualPS <= 0 {
				return // 배당 없는 종목이거나 조회 실패
			}
			snap, ok := m.snaps[sym]
			if !ok || snap.Price <= 0 {
				return
			}
			holding, ok2 := service.DividendPlanFor(sym, merged[sym].shares, snap.Price, hist)
			if !ok2 {
				return
			}
			c := models.DividendCandidate{
				Symbol:      sym,
				Shares:      merged[sym].shares,
				AnnualDPS:   hist.AnnualPS,
				Yield:       hist.AnnualPS / snap.Price,
				CAGR3Y:      hist.CAGR3Y,
				AutoInclude: service.MeetsDividendYieldFloor(hist.AnnualPS, snap.Price),
				Holding:     holding,
			}
			// 고른 종목이 있으면 그 목록이 기준이고, 없으면 배당률로 자동 판정한다.
			if len(chosen) > 0 {
				c.Selected = chosen[sym]
			} else {
				c.Selected = c.AutoInclude
			}
			mu.Lock()
			out = append(out, c)
			mu.Unlock()
		}(sym)
	}
	wg.Wait()

	sort.Slice(out, func(i, j int) bool { return out[i].Symbol < out[j].Symbol })
	return out, nil
}

// dividendPlanFrom은 고른 후보만 배당 계획으로 모은다.
func dividendPlanFrom(cands []models.DividendCandidate) []models.DividendHolding {
	plan := make([]models.DividendHolding, 0, len(cands))
	for _, c := range cands {
		if c.Selected {
			plan = append(plan, c.Holding)
		}
	}
	return plan
}

// contributionsFrom은 적립 실적을 세기 시작하는 시점이다.
//
// 그 전 기록에는 앱을 쓰기 전부터 갖고 있던 주식을 등록한 매수가 섞여 있어,
// 2026년 8월 한 달 적립이 2.5억으로 잡힌다. 실제로 넣은 돈이 아니다.
var contributionsFrom = time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)

// roadmapProjection — GET /api/roadmap/projection
//
// 계획(가정대로 굴린 궤적)과 실적(스냅샷)을 한 번에 준다. 필요 수익률은
// 목표일에 목표액이 되는 가격상승률을 역산한 값이다.
func (h *Handler) roadmapProjection(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	coupleID := auth.CoupleIDFromCtx(ctx)

	// 환율·보유 종목·시세는 한 번만 읽어 순자산과 배당 계획이 함께 쓴다.
	// 목표·가정, 실적 스냅샷은 그와 무관하니 같이 읽는다.
	var (
		goal            *models.RoadmapGoal
		a               *models.RoadmapAssumptions
		market          *marketView
		snapshots       []models.NetWorthSnapshot
		actualContrib   map[string]int64
		goalErr, mktErr error
		snapErr         error
		contribErr      error
		wg              sync.WaitGroup
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
		market, mktErr = h.loadMarket(ctx, coupleID)
	}()
	go func() {
		defer wg.Done()
		snapshots, snapErr = h.roadmapRepo.ListSnapshots(ctx, coupleID)
	}()
	go func() {
		defer wg.Done()
		actualContrib, contribErr = h.roadmapRepo.MonthlyContributions(ctx, coupleID, contributionsFrom)
	}()
	wg.Wait()

	for _, err := range []error{goalErr, mktErr, snapErr, contribErr} {
		if err != nil {
			respondError(w, http.StatusInternalServerError, err)
			return
		}
	}

	stockKRW, assetKRW, liabilityKRW := market.netWorth()
	divCands, err := h.dividendCandidates(ctx, market, a.DividendSymbols)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	// 배당 계획은 저장값이 아니라 지금 보유 종목에서 만든 것을 쓴다.
	a.DividendPlan = dividendPlanFrom(divCands)

	// 기타 자산은 저장된 가정이 있으면 그 값을, 없으면 실제 보유분을 쓴다.
	otherFromAssumption := a.OtherAssetsKRW != 0
	if a.OtherAssetsKRW == 0 {
		a.OtherAssetsKRW = assetKRW - liabilityKRW
	}
	netWorthKRW := stockKRW + a.OtherAssetsKRW

	usdKRW := market.usdKRW

	now := time.Now().UTC()

	// 이번 달 실적을 남긴다. 조회할 때마다 덮어쓰므로 그 달에 마지막으로 연
	// 시점의 값이 기록되고, 달이 지나면 그 값으로 굳는다. 기록할 곳이 없으면
	// 지난 달들의 실적이 영영 빈칸으로 남는다.
	//
	// 응답을 늦추지 않으려고 뒤에서 처리한다. 요청 컨텍스트는 응답과 함께
	// 끊기므로 쓰지 않는다.
	go func(snap *models.NetWorthSnapshot) {
		bg, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, err := h.roadmapRepo.UpsertSnapshot(bg, snap); err != nil {
			fmt.Printf("warn: 순자산 스냅샷 적재 실패: %v\n", err)
		}
	}(newSnapshot(coupleID, stockKRW, assetKRW, liabilityKRW, now))

	// 계획선은 확정된 기준선에서 읽는다. 실시간 순자산으로 다시 굴리면 목표가
	// 자산을 따라 움직여, 계획보다 앞섰는지 뒤처졌는지 알 수 없다.
	baseline, err := h.baselineFor(r, goal, *a, otherFromAssumption, snapshots, stockKRW, usdKRW, now)
	if err != nil {
		respondError(w, http.StatusInternalServerError, err)
		return
	}
	years, months := planFromBaseline(*baseline)

	// 필요 수익률은 지금 순자산에서 목표까지 남은 기간에 필요한 값이다. 계획보다
	// 뒤처지면 오르고 앞서면 내려간다. 계획선은 그대로 둔다.
	growth := service.SolveRequiredGrowth(*a, stockKRW, usdKRW, goal.TargetKRW, now, goal.TargetDate, goal.BirthYear)

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
		if v, ok := actualContrib[months[i].Month]; ok {
			contributed := v
			months[i].ActualContributionKRW = &contributed
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
	out.Plan.AnchorMonth = baseline.AnchorMonth.Format("2006-01")
	out.Plan.AnchorNetWorthKRW = baseline.AnchorNetWorthKRW
	out.Plan.PriceGrowth = baseline.PriceGrowth
	out.Plan.TotalReturn = baseline.PriceGrowth + baseline.DividendYield
	out.RequiredPriceGrowth = growth
	out.CurrentDividendYield = service.DividendYield(*a, stockKRW, usdKRW, now.Year())
	out.RequiredTotalReturn = growth + out.CurrentDividendYield
	out.Years = years
	out.Months = months

	respondJSON(w, http.StatusOK, out)
}

// baselineFor는 목표의 확정 계획선을 준다. 없으면 이번 달을 출발점으로 한 번
// 만들어 저장한다.
//
// 출발 자산은 이번 달 스냅샷이 있으면 그 값을 쓴다. 스냅샷은 그 달에 처음 연
// 뒤로 기록돼 있어, 지금 시세로 다시 재는 것보다 출발점이 덜 흔들린다.
func (h *Handler) baselineFor(r *http.Request, goal *models.RoadmapGoal, a models.RoadmapAssumptions, otherFromAssumption bool, snapshots []models.NetWorthSnapshot, stockKRW int64, fx float64, now time.Time) (*models.RoadmapBaseline, error) {
	ctx := r.Context()
	if goal.ID == "" { // 기본 목표는 저장돼 있지 않다. 계획선을 달려면 먼저 저장한다
		saved, err := h.ensureGoal(r, goal.CoupleID)
		if err != nil {
			return nil, err
		}
		goal = saved
	}
	b, err := h.roadmapRepo.Baseline(ctx, goal.ID)
	if err != nil || b != nil {
		return b, err
	}

	anchor := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	for _, s := range snapshots {
		if s.SnapshotMonth.Equal(anchor) {
			stockKRW = s.StockKRW
			if !otherFromAssumption {
				a.OtherAssetsKRW = s.AssetKRW - s.LiabilityKRW
			}
		}
	}
	nb := service.NewBaseline(a, stockKRW, fx, goal.TargetKRW, anchor, goal.TargetDate, goal.BirthYear)
	nb.GoalID = goal.ID
	nb.CoupleID = goal.CoupleID
	if err := h.roadmapRepo.SaveBaseline(ctx, &nb); err != nil {
		return nil, err
	}
	// 동시에 다른 요청이 먼저 저장했을 수 있다. 저장된 쪽을 따른다.
	return h.roadmapRepo.Baseline(ctx, goal.ID)
}

// planFromBaseline은 확정 계획선을 화면용 궤적으로 편다. 출발 월을 첫 줄로
// 붙여 계획과 실적이 같은 지점에서 갈라져 나가게 한다. 실적을 채우느라
// 결과를 고쳐도 기준선은 그대로 남도록 복사해서 준다.
func planFromBaseline(b models.RoadmapBaseline) ([]models.RoadmapYearRow, []models.RoadmapMonthPoint) {
	years := append([]models.RoadmapYearRow(nil), b.Years...)
	months := make([]models.RoadmapMonthPoint, 0, len(b.Months)+1)
	months = append(months, models.RoadmapMonthPoint{
		Month:                b.AnchorMonth.Format("2006-01"),
		ProjectedNetWorthKRW: b.AnchorNetWorthKRW,
	})
	months = append(months, b.Months...)
	return years, months
}
