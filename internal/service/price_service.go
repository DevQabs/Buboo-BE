// Package service provides real-time stock price and exchange rate fetching
// via Yahoo Finance's public chart API (no API key required).
//
// Cache TTL: 5 minutes — prevents hammering the API on every portfolio request.
// Fallback: if the live fetch fails, the caller falls back to the stored snapshot in stocks.json.
package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/yourname/couple-app/internal/models"
)

const (
	yahooBaseURL = "https://query1.finance.yahoo.com/v8/finance/chart"
	cacheTTL     = 5 * time.Minute
	httpTimeout  = 10 * time.Second

	// FallbackUSDKRW is used when the live FX fetch fails.
	FallbackUSDKRW = 1380.0
)

// ─────────────────────────────────────────────
//  Internal cache types
// ─────────────────────────────────────────────

type cachedPrice struct {
	snap      models.PriceSnapshot
	fetchedAt time.Time
}

type cachedFX struct {
	rate      float64
	fetchedAt time.Time
}

// ─────────────────────────────────────────────
//  PriceService
// ─────────────────────────────────────────────

// PriceService fetches real-time prices from Yahoo Finance.
// All results are cached in memory for cacheTTL to avoid repeated API calls.
type PriceService struct {
	client  *http.Client
	mu      sync.RWMutex
	prices  map[string]cachedPrice // "SYMBOL:EXCHANGE" → cached snap
	fxCache *cachedFX
}

// NewPriceService creates a PriceService with a shared HTTP client.
func NewPriceService() *PriceService {
	return &PriceService{
		client: &http.Client{Timeout: httpTimeout},
		prices: make(map[string]cachedPrice),
	}
}

// ─────────────────────────────────────────────
//  Yahoo Finance response structs
// ─────────────────────────────────────────────

type yahooMeta struct {
	RegularMarketPrice         float64 `json:"regularMarketPrice"`
	PreviousClose              float64 `json:"previousClose"`
	RegularMarketPreviousClose float64 `json:"regularMarketPreviousClose"`
	Currency                   string  `json:"currency"`
	ExchangeName               string  `json:"exchangeName"`
}

type yahooResult struct {
	Meta yahooMeta `json:"meta"`
}

type yahooChart struct {
	Result []yahooResult `json:"result"`
	Error  *struct {
		Code        string `json:"code"`
		Description string `json:"description"`
	} `json:"error"`
}

type yahooResponse struct {
	Chart yahooChart `json:"chart"`
}

// ─────────────────────────────────────────────
//  Symbol mapping
// ─────────────────────────────────────────────

// toYahooSymbol converts our internal symbol+exchange to Yahoo Finance format.
//   - KRX:   "005930" → "005930.KS"
//   - KOSDAQ: "035720" → "035720.KQ"  (Kakao is actually KRX but left for flexibility)
//   - Others: returned as-is (AAPL, TSLA, etc.)
func toYahooSymbol(symbol, exchange string) string {
	switch exchange {
	case "KRX":
		return symbol + ".KS"
	case "KOSDAQ":
		return symbol + ".KQ"
	default:
		return symbol
	}
}

// ─────────────────────────────────────────────
//  FetchPrice
// ─────────────────────────────────────────────

// FetchPrice returns a live PriceSnapshot for the given symbol/exchange.
// Results are cached for cacheTTL. On error, returns the error so the caller
// can fall back to the last stored snapshot.
func (p *PriceService) FetchPrice(ctx context.Context, symbol, exchange string) (*models.PriceSnapshot, error) {
	cacheKey := symbol + ":" + exchange

	// Fast path: cache hit
	p.mu.RLock()
	if cached, ok := p.prices[cacheKey]; ok && time.Since(cached.fetchedAt) < cacheTTL {
		snap := cached.snap
		p.mu.RUnlock()
		return &snap, nil
	}
	p.mu.RUnlock()

	// Slow path: live fetch
	snap, err := p.fetchFromYahoo(ctx, symbol, exchange)
	if err != nil {
		return nil, err
	}

	p.mu.Lock()
	p.prices[cacheKey] = cachedPrice{snap: *snap, fetchedAt: time.Now()}
	p.mu.Unlock()

	return snap, nil
}

// FetchUSDKRW returns the current USD/KRW exchange rate.
// Falls back to FallbackUSDKRW if the request fails.
func (p *PriceService) FetchUSDKRW(ctx context.Context) (float64, error) {
	p.mu.RLock()
	if p.fxCache != nil && time.Since(p.fxCache.fetchedAt) < cacheTTL {
		rate := p.fxCache.rate
		p.mu.RUnlock()
		return rate, nil
	}
	p.mu.RUnlock()

	url := fmt.Sprintf("%s/USDKRW=X?interval=1d&range=1d", yahooBaseURL)
	var yr yahooResponse
	if err := p.doRequest(ctx, url, &yr); err != nil {
		return 0, err
	}
	if yr.Chart.Error != nil {
		return 0, fmt.Errorf("yahoo FX error %s: %s", yr.Chart.Error.Code, yr.Chart.Error.Description)
	}
	if len(yr.Chart.Result) == 0 {
		return 0, fmt.Errorf("no FX data returned")
	}

	rate := yr.Chart.Result[0].Meta.RegularMarketPrice

	p.mu.Lock()
	p.fxCache = &cachedFX{rate: rate, fetchedAt: time.Now()}
	p.mu.Unlock()

	return rate, nil
}

// FetchUSDKRWWithFallback calls FetchUSDKRW but returns FallbackUSDKRW on error.
func (p *PriceService) FetchUSDKRWWithFallback(ctx context.Context) float64 {
	rate, err := p.FetchUSDKRW(ctx)
	if err != nil || rate == 0 {
		return FallbackUSDKRW
	}
	return rate
}

// InvalidateCache removes cached entries for a symbol (useful for /refresh endpoint).
func (p *PriceService) InvalidateCache(symbol, exchange string) {
	p.mu.Lock()
	delete(p.prices, symbol+":"+exchange)
	p.mu.Unlock()
}

// InvalidateAll clears the entire price + FX cache.
func (p *PriceService) InvalidateAll() {
	p.mu.Lock()
	p.prices = make(map[string]cachedPrice)
	p.fxCache = nil
	p.mu.Unlock()
}

// ─────────────────────────────────────────────
//  Internal helpers
// ─────────────────────────────────────────────

func (p *PriceService) fetchFromYahoo(ctx context.Context, symbol, exchange string) (*models.PriceSnapshot, error) {
	ys := toYahooSymbol(symbol, exchange)
	url := fmt.Sprintf("%s/%s?interval=1d&range=1d", yahooBaseURL, ys)

	var yr yahooResponse
	if err := p.doRequest(ctx, url, &yr); err != nil {
		return nil, err
	}
	if yr.Chart.Error != nil {
		return nil, fmt.Errorf("yahoo error [%s] %s", yr.Chart.Error.Code, yr.Chart.Error.Description)
	}
	if len(yr.Chart.Result) == 0 {
		return nil, fmt.Errorf("no data for symbol %s (%s)", symbol, ys)
	}

	meta := yr.Chart.Result[0].Meta
	price := meta.RegularMarketPrice

	// Prefer regularMarketPreviousClose, fall back to previousClose
	prevClose := meta.RegularMarketPreviousClose
	if prevClose == 0 {
		prevClose = meta.PreviousClose
	}

	change := price - prevClose
	var changePct float64
	if prevClose > 0 {
		changePct = (change / prevClose) * 100
	}

	return &models.PriceSnapshot{
		Symbol:        symbol,
		Exchange:      exchange,
		Price:         price,
		Currency:      meta.Currency,
		Change:        change,
		ChangePercent: changePct,
		SnapshottedAt: time.Now().UTC(),
	}, nil
}

func (p *PriceService) doRequest(ctx context.Context, url string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	// Yahoo Finance returns 429 without a browser-like User-Agent
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36")
	req.Header.Set("Accept", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("yahoo returned HTTP %d for %s", resp.StatusCode, url)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}
