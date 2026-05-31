// Package models defines the core domain structs for the couple's
// household budget and asset management application.
// All structs use json tags for clean serialization and are designed
// to be DB-agnostic for future PostgreSQL migration.
package models

import "time"

// ─────────────────────────────────────────────
//  Schedule (일정)
// ─────────────────────────────────────────────

type Schedule struct {
	ID          string     `json:"id"`
	CoupleID    string     `json:"couple_id"`
	UserID      string     `json:"user_id"`
	Title       string     `json:"title"`
	Description string     `json:"description"`
	StartDate   time.Time  `json:"start_date"`
	EndDate     *time.Time `json:"end_date,omitempty"`
	AllDay      bool       `json:"all_day"`
	IsDDay      bool       `json:"is_dday"`
	DDayLabel   string     `json:"dday_label"`
	Color       string     `json:"color"` // "indigo"|"rose"|"emerald"|"amber"|"sky"
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type SchedulesFile struct {
	Schedules []Schedule `json:"schedules"`
}

type CreateScheduleRequest struct {
	UserID      string  `json:"user_id"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	StartDate   string  `json:"start_date"` // YYYY-MM-DD
	EndDate     *string `json:"end_date,omitempty"`
	AllDay      bool    `json:"all_day"`
	IsDDay      bool    `json:"is_dday"`
	DDayLabel   string  `json:"dday_label"`
	Color       string  `json:"color"`
}

type UpdateScheduleRequest struct {
	Title       *string `json:"title,omitempty"`
	Description *string `json:"description,omitempty"`
	StartDate   *string `json:"start_date,omitempty"`
	EndDate     *string `json:"end_date,omitempty"`
	AllDay      *bool   `json:"all_day,omitempty"`
	IsDDay      *bool   `json:"is_dday,omitempty"`
	DDayLabel   *string `json:"dday_label,omitempty"`
	Color       *string `json:"color,omitempty"`
}

// ─────────────────────────────────────────────
//  DiaryEntry (일기)
// ─────────────────────────────────────────────

type DiaryEntry struct {
	ID        string    `json:"id"`
	CoupleID  string    `json:"couple_id"`
	UserID    string    `json:"user_id"`
	Date      string    `json:"date"`    // YYYY-MM-DD
	Content   string    `json:"content"`
	Photos    []string  `json:"photos"`  // filenames under uploads/
	Mood      string    `json:"mood"`    // "happy"|"good"|"normal"|"sad"|"tired"
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type DiariesFile struct {
	Diaries []DiaryEntry `json:"diaries"`
}

type CreateDiaryRequest struct {
	UserID  string `json:"user_id"`
	Date    string `json:"date"` // YYYY-MM-DD
	Content string `json:"content"`
	Mood    string `json:"mood"`
}

type UpdateDiaryRequest struct {
	Content *string `json:"content,omitempty"`
	Mood    *string `json:"mood,omitempty"`
}

// ─────────────────────────────────────────────
//  User & Couple
// ─────────────────────────────────────────────

// User represents one of the two spouses.
type User struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Email       string    `json:"email"`
	Role        string    `json:"role"`         // "husband" | "wife"
	AvatarColor string    `json:"avatar_color"` // hex color for UI
	CreatedAt   time.Time `json:"created_at"`
}

// Couple is the shared household entity that links two users.
type Couple struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	MonthlyBudget  int64     `json:"monthly_budget"`   // 단위: 원(KRW)
	LedgerStartDay int       `json:"ledger_start_day"` // 가계부 기간 시작일 (1-28, 기본값 1)
	Currency       string    `json:"currency"`
	CreatedAt      time.Time `json:"created_at"`
}

// UsersFile mirrors the top-level structure of users.json.
type UsersFile struct {
	Users  []User `json:"users"`
	Couple Couple `json:"couple"`
}

// ─────────────────────────────────────────────
//  Transaction (가계부 거래 내역)
// ─────────────────────────────────────────────

// Location holds optional geolocation data for a transaction.
// Both fields are pointers (nullable) to support future map API integration.
type Location struct {
	Name    string   `json:"name"`
	Lat     *float64 `json:"lat"`     // nullable – OSM/Naver 지도 연동 예정
	Lng     *float64 `json:"lng"`     // nullable – OSM/Naver 지도 연동 예정
	Address string   `json:"address"`
}

// SavingKind identifies the target asset type for a saving transaction.
type SavingKind string

const (
	SavingKindStock   SavingKind = "stock"   // links to stock_assets
	SavingKindDeposit SavingKind = "deposit" // links to other_assets (예금/적금)
	SavingKindGeneral SavingKind = "general" // links to other_assets (기타)
)

// SavingLink embeds saving metadata in Transaction and FixedExpense.
// LinkAssetID empty = create new asset; non-empty = add to existing.
type SavingLink struct {
	Kind        SavingKind `json:"kind"`
	LinkAssetID string     `json:"link_asset_id,omitempty"`

	// Stock: add to existing (LinkAssetID != "")
	AddStockQty   float64 `json:"add_stock_qty,omitempty"`
	AddStockPrice float64 `json:"add_stock_price,omitempty"` // per share

	// Stock: create new (LinkAssetID == "")
	NewStockSymbol   string `json:"new_stock_symbol,omitempty"`
	NewStockExchange string `json:"new_stock_exchange,omitempty"`
	NewStockName     string `json:"new_stock_name,omitempty"`
	NewStockQty      float64 `json:"new_stock_qty,omitempty"`
	NewStockPrice    float64 `json:"new_stock_price,omitempty"` // avg price per share
	NewStockCurrency string  `json:"new_stock_currency,omitempty"`
	NewStockSector   string  `json:"new_stock_sector,omitempty"`

	// Other asset: create new (LinkAssetID == "")
	NewAssetType OtherAssetType `json:"new_asset_type,omitempty"`
	NewAssetName string         `json:"new_asset_name,omitempty"`
}

// Transaction represents a single income, expense, or saving entry.
type Transaction struct {
	ID             string      `json:"id"`
	CoupleID       string      `json:"couple_id"`
	UserID         string      `json:"user_id"`
	Type           string      `json:"type"`            // "income" | "expense" | "saving"
	Amount         int64       `json:"amount"`          // 단위: 원(KRW) 또는 외화 최소 단위
	Currency       string      `json:"currency"`        // "KRW" | "USD" 등
	Category       string      `json:"category"`        // 대분류: "식비", "급여" 등
	Subcategory    string      `json:"subcategory"`     // 소분류
	Title          string      `json:"title"`
	Memo           string      `json:"memo"`
	Date           time.Time   `json:"date"`
	PaymentMethod  string      `json:"payment_method"`  // "신용카드" | "체크카드" | "현금" 등
	IsFixed        bool        `json:"is_fixed"`        // 고정 지출/수입 여부
	Tags           []string    `json:"tags"`
	Location       *Location   `json:"location"`        // nullable – 지도 미연동 시 null
	FixedExpenseID *string     `json:"fixed_expense_id,omitempty"` // 고정비 자동 생성 시 연결 ID
	SavingLink     *SavingLink `json:"saving_link,omitempty"`      // non-nil when type=="saving"
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

// TransactionsFile mirrors the top-level structure of transactions.json.
type TransactionsFile struct {
	Transactions []Transaction `json:"transactions"`
}

// CreateTransactionRequest is the DTO for POST /transactions.
type CreateTransactionRequest struct {
	UserID        string      `json:"user_id"`
	Type          string      `json:"type"`
	Amount        int64       `json:"amount"`
	Currency      string      `json:"currency"`
	Category      string      `json:"category"`
	Subcategory   string      `json:"subcategory"`
	Title         string      `json:"title"`
	Memo          string      `json:"memo"`
	Date          time.Time   `json:"date"`
	PaymentMethod string      `json:"payment_method"`
	IsFixed       bool        `json:"is_fixed"`
	Tags          []string    `json:"tags"`
	Location      *Location   `json:"location"`
	SavingLink    *SavingLink `json:"saving_link,omitempty"` // required when type=="saving"
}

// MonthlySummary is a computed DTO returned by the summary endpoint.
type MonthlySummary struct {
	Year         int   `json:"year"`
	Month        int   `json:"month"`
	TotalIncome  int64 `json:"total_income"`
	TotalExpense int64 `json:"total_expense"`
	Balance      int64 `json:"balance"`
	BudgetLimit  int64 `json:"budget_limit"`
}

// ─────────────────────────────────────────────
//  Stock Asset (주식 자산)
// ─────────────────────────────────────────────

// StockAsset represents a single stock holding by one of the spouses.
type StockAsset struct {
	ID           string    `json:"id"`
	CoupleID     string    `json:"couple_id"`
	UserID       string    `json:"user_id"`
	Symbol       string    `json:"symbol"`       // 종목 코드, e.g. "005930", "AAPL"
	Exchange     string    `json:"exchange"`     // "KRX" | "NASDAQ" | "NYSE" 등
	Name         string    `json:"name"`         // 한글 종목명
	NameEn       string    `json:"name_en"`      // 영문 종목명
	Quantity     float64   `json:"quantity"`     // 보유 수량 (소수 허용: ETF 등)
	AveragePrice float64   `json:"average_price"` // 평균 매입가
	Currency     string    `json:"currency"`     // 거래 통화
	Sector       string    `json:"sector"`       // 섹터 분류
	Memo         string    `json:"memo"`
	LogoURL      *string   `json:"logo_url"`     // nullable – 추후 CDN 연동
	PurchasedAt  time.Time `json:"purchased_at"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// PriceSnapshot holds the latest fetched price for a symbol.
// In MVP this is stored in stocks.json; later it will come from a real API.
type PriceSnapshot struct {
	Symbol          string    `json:"symbol"`
	Exchange        string    `json:"exchange"`
	Price           float64   `json:"price"`
	Currency        string    `json:"currency"`
	Change          float64   `json:"change"`          // 전일 대비 변동액
	ChangePercent   float64   `json:"change_percent"`  // 전일 대비 변동률 (%)
	SnapshottedAt   time.Time `json:"snapshotted_at"`
}

// StockAssetsFile mirrors the top-level structure of stocks.json.
type StockAssetsFile struct {
	StockAssets    []StockAsset    `json:"stock_assets"`
	PriceSnapshots []PriceSnapshot `json:"price_snapshots"`
}

// StockAssetWithPrice is a computed view joining StockAsset + live price.
// This is what the frontend receives to render the portfolio.
type StockAssetWithPrice struct {
	StockAsset
	CurrentPrice    float64   `json:"current_price"`
	CurrentValue    float64   `json:"current_value"`     // quantity × current_price (original currency)
	CurrentValueKRW float64   `json:"current_value_krw"` // converted to KRW for unified display
	ProfitLoss      float64   `json:"profit_loss"`       // current_value − (quantity × avg_price), original currency
	ProfitLossKRW   float64   `json:"profit_loss_krw"`   // profit_loss converted to KRW
	ProfitLossPct   float64   `json:"profit_loss_pct"`   // profit_loss / cost_basis × 100
	Change          float64   `json:"change"`
	ChangePercent   float64   `json:"change_percent"`
	ExchangeRate    float64   `json:"exchange_rate,omitempty"` // USD/KRW rate used (only for USD assets)
	PriceSource     string    `json:"price_source"`            // "live" | "cached"
	PriceUpdatedAt  time.Time `json:"price_updated_at"`
}

// PortfolioSummary is the aggregate view returned alongside portfolio items.
type PortfolioSummary struct {
	TotalValueKRW    float64   `json:"total_value_krw"`
	TotalCostKRW     float64   `json:"total_cost_krw"`
	TotalProfitKRW   float64   `json:"total_profit_krw"`
	TotalProfitPct   float64   `json:"total_profit_pct"`
	USDKRW           float64   `json:"usd_krw"`
	FXSource         string    `json:"fx_source"` // "live" | "fallback"
	CalculatedAt     time.Time `json:"calculated_at"`
}

// CreateStockRequest is the validated DTO for POST /api/stocks.
type CreateStockRequest struct {
	UserID       string     `json:"user_id"`
	Symbol       string     `json:"symbol"`
	Exchange     string     `json:"exchange"`
	Name         string     `json:"name"`
	NameEn       string     `json:"name_en"`
	Quantity     float64    `json:"quantity"`
	AveragePrice float64    `json:"average_price"`
	Currency     string     `json:"currency"`
	Sector       string     `json:"sector"`
	Memo         string     `json:"memo"`
	PurchasedAt  *time.Time `json:"purchased_at"` // nullable — defaults to now
}

// UpdateStockRequest is the validated DTO for PUT /api/stocks/{id}.
// All fields are optional (pointer types) — only non-nil fields are updated.
type UpdateStockRequest struct {
	UserID       *string    `json:"user_id"`
	Name         *string    `json:"name"`
	Quantity     *float64   `json:"quantity"`
	AveragePrice *float64   `json:"average_price"`
	Sector       *string    `json:"sector"`
	Memo         *string    `json:"memo"`
}

// ─────────────────────────────────────────────
//  StockTransaction (매수/매도 이력 — Immutable Log)
// ─────────────────────────────────────────────

// StockTxType distinguishes buy from sell events.
type StockTxType string

const (
	StockTxBuy  StockTxType = "buy"
	StockTxSell StockTxType = "sell"
)

// StockTransaction is an immutable record of a single buy or sell event.
// It is NEVER updated or deleted — it is the audit log for tax calculation.
type StockTransaction struct {
	ID             string      `json:"id"`
	CoupleID       string      `json:"couple_id"`
	UserID         string      `json:"user_id"`
	StockAssetID   string      `json:"stock_asset_id"` // "" if asset was later deleted
	Symbol         string      `json:"symbol"`
	Exchange       string      `json:"exchange"`
	Name           string      `json:"name"`
	Type           StockTxType `json:"type"`            // "buy" | "sell"
	Quantity       float64     `json:"quantity"`
	Price          float64     `json:"price"`           // per share, original currency
	Currency       string      `json:"currency"`
	AvgPriceAtTx   float64     `json:"avg_price_at_tx"` // avg cost basis at time of tx
	RealizedPnL    float64     `json:"realized_pnl"`    // sell only: (price - avg) * qty; 0 for buy
	Memo           string      `json:"memo"`
	ExecutedAt     time.Time   `json:"executed_at"`
	CreatedAt      time.Time   `json:"created_at"`
}

// StockTransactionsFile mirrors the top-level structure of stock_transactions.json.
type StockTransactionsFile struct {
	Transactions []StockTransaction `json:"transactions"`
}

// BuyRequest is the DTO for POST /api/stocks/{id}/buy.
type BuyRequest struct {
	Quantity float64 `json:"quantity"`
	Price    float64 `json:"price"`
	Memo     string  `json:"memo"`
}

// SellRequest is the DTO for POST /api/stocks/{id}/sell.
type SellRequest struct {
	Quantity float64 `json:"quantity"`
	Price    float64 `json:"price"`
	Memo     string  `json:"memo"`
}

// TaxCheckResponse is returned by GET /api/stocks/{id}/tax-check.
type TaxCheckResponse struct {
	HasSellCurrentYear bool `json:"has_sell_current_year"`
	Year               int  `json:"year"`
}

// AnnualTaxSummary is computed on-the-fly from stock_transactions.
// US 22% self-reporting basis (해외주식 양도소득세).
type AnnualTaxSummary struct {
	Year             int                `json:"year"`
	CoupleID         string             `json:"couple_id"`
	TotalRealizedPnL float64            `json:"total_realized_pnl"` // USD
	TaxableGain      float64            `json:"taxable_gain"`       // max(0, total_realized_pnl)
	EstimatedTax     float64            `json:"estimated_tax"`      // taxable_gain * 0.22
	TaxRate          float64            `json:"tax_rate"`           // 0.22
	BySymbol         []SymbolTaxSummary `json:"by_symbol"`
}

// SymbolTaxSummary aggregates realized P&L for a single symbol in a given year.
type SymbolTaxSummary struct {
	Symbol       string  `json:"symbol"`
	Exchange     string  `json:"exchange"`
	SellCount    int     `json:"sell_count"`
	RealizedPnL  float64 `json:"realized_pnl"`  // USD
	EstimatedTax float64 `json:"estimated_tax"` // max(0, realized_pnl) * 0.22
}

// ─────────────────────────────────────────────
//  OtherAsset (기타 자산 — 부동산·예금·현금·대출 등)
// ─────────────────────────────────────────────

// OtherAssetType categorises non-stock assets.
type OtherAssetType string

const (
	AssetTypeRealEstate OtherAssetType = "부동산"  // 아파트, 토지 등
	AssetTypeDeposit    OtherAssetType = "예/적금" // 정기예금, 적금
	AssetTypeCash       OtherAssetType = "현금"   // 현금성 자산 (KRW/USD)
	AssetTypeLoan       OtherAssetType = "대출"   // 부채 — 주택담보대출, 신용대출 등
	AssetTypeOther      OtherAssetType = "기타"   // 미분류
)

// OtherAsset represents a non-stock asset entry (real estate, deposits, etc.).
type OtherAsset struct {
	ID          string         `json:"id"`
	CoupleID    string         `json:"couple_id"`
	UserID      string         `json:"user_id"`
	AssetType   OtherAssetType `json:"asset_type"`  // 자산 유형
	Name        string         `json:"name"`         // 자산명
	Description string         `json:"description"`  // 상세 설명
	ValueKRW    int64          `json:"value_krw"`    // 현재 평가액 (원) — USD 현금은 환율 적용 후 값
	ValueUSD    *float64       `json:"value_usd"`    // USD 현금 전용: 원본 USD 금액
	CostKRW     int64          `json:"cost_krw"`     // 취득 원가 (원)
	Currency    string         `json:"currency"`     // "KRW" | "USD"
	IsLiability bool           `json:"is_liability"` // 대출이면 true (자동 결정)
	IsLocked    bool           `json:"is_locked"`    // true이면 인출/처분 불가 자산 (비유동)
	// 부동산 전용
	Location *Location `json:"location"`
	// 예/적금·대출 전용
	MaturityDate *time.Time `json:"maturity_date"` // 만기일 (nullable)
	InterestRate *float64   `json:"interest_rate"` // 연이율 % (nullable)
	// 대출 전용
	LoanType   string `json:"loan_type"`   // "만기일시상환" | "원리금균등상환" | "원금균등상환"
	PaymentDay int    `json:"payment_day"` // 납입일 (1-28)
	// 공통
	Memo       string    `json:"memo"`
	AcquiredAt time.Time `json:"acquired_at"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// OtherAssetsFile mirrors the top-level structure of other_assets.json.
type OtherAssetsFile struct {
	OtherAssets []OtherAsset `json:"other_assets"`
}

// CreateOtherAssetRequest is the DTO for POST /api/assets.
type CreateOtherAssetRequest struct {
	UserID       string         `json:"user_id"`
	AssetType    OtherAssetType `json:"asset_type"`
	Name         string         `json:"name"`
	Description  string         `json:"description"`
	ValueKRW     int64          `json:"value_krw"`
	ValueUSD     *float64       `json:"value_usd"`  // USD 현금 전용
	CostKRW      int64          `json:"cost_krw"`
	Currency     string         `json:"currency"`
	IsLocked     bool           `json:"is_locked"`
	Location     *Location      `json:"location"`
	MaturityDate *time.Time     `json:"maturity_date"`
	InterestRate *float64       `json:"interest_rate"`
	LoanType     string         `json:"loan_type"`
	PaymentDay   int            `json:"payment_day"`
	Memo         string         `json:"memo"`
	AcquiredAt   *time.Time     `json:"acquired_at"` // nullable — defaults to now
}

// UpdateOtherAssetRequest is the DTO for PUT /api/assets/{id}.
// 모든 필드 optional — 전달된 필드만 덮어씀 (partial update).
type UpdateOtherAssetRequest struct {
	AssetType    *OtherAssetType `json:"asset_type"`
	Name         *string         `json:"name"`
	Description  *string         `json:"description"`
	ValueKRW     *int64          `json:"value_krw"`
	ValueUSD     *float64        `json:"value_usd"` // USD 현금 전용
	CostKRW      *int64          `json:"cost_krw"`
	IsLocked     *bool           `json:"is_locked"`
	Location     *Location       `json:"location"`
	MaturityDate *time.Time      `json:"maturity_date"`
	InterestRate *float64        `json:"interest_rate"`
	LoanType     *string         `json:"loan_type"`
	PaymentDay   *int            `json:"payment_day"`
	Memo         *string         `json:"memo"`
}

// ─────────────────────────────────────────────
//  Calendar (일별 집계 — 캘린더 뷰)
// ─────────────────────────────────────────────

// CalendarDay holds the aggregated income/expense totals for a single date.
// This is pre-computed on the backend (GROUP BY date) to avoid sending
// all transaction records to the client just to sum them in the browser.
type CalendarDay struct {
	Date             string `json:"date"`              // "YYYY-MM-DD"
	TotalExpense     int64  `json:"total_expense"`     // 해당 일 총 지출
	TotalIncome      int64  `json:"total_income"`      // 해당 일 총 수입
	TransactionCount int    `json:"transaction_count"` // 거래 건수
}

// CalendarEvent represents a non-transaction event shown as a dot on the calendar.
// Sources: fixed expenses (예정 이체일) and dividend payouts (배당 지급일).
type CalendarEvent struct {
	Date   string `json:"date"`   // "YYYY-MM-DD"
	Type   string `json:"type"`   // "fixed_expense" | "dividend"
	Title  string `json:"title"`  // 표시용 명칭
	Amount int64  `json:"amount"` // 금액 (KRW)
}

// CalendarSummaryResponse is the response for GET /api/transactions/calendar-summary.
// Designed for O(days) rendering: the client maps days[] by date and looks up
// each grid cell directly, with no further aggregation needed.
type CalendarSummaryResponse struct {
	Year   int             `json:"year"`
	Month  int             `json:"month"`
	Days   []CalendarDay   `json:"days"`   // only days with at least one transaction
	Events []CalendarEvent `json:"events"` // fixed expense + dividend dots
}

// ─────────────────────────────────────────────
//  DividendEvent (배당 이벤트)
// ─────────────────────────────────────────────

// DividendEvent records a single dividend payout for a held stock.
// Amounts are stored in both the original currency (USD) and KRW equivalent
// so the ledger entry can be created without re-calling the exchange rate API.
type DividendEvent struct {
	ID             string     `json:"id"`
	CoupleID       string     `json:"couple_id"`
	UserID         string     `json:"user_id"`
	StockAssetID   string     `json:"stock_asset_id"` // links to portfolio entry
	Symbol         string     `json:"symbol"`
	Exchange       string     `json:"exchange"`
	Name           string     `json:"name"`
	Quantity       float64    `json:"quantity"`           // shares held at record date
	AmountPerShare float64    `json:"amount_per_share"`   // dividend per share (original currency)
	Currency       string     `json:"currency"`           // "USD" etc.
	TotalAmount    float64    `json:"total_amount"`       // quantity × amount_per_share
	TaxRate        float64    `json:"tax_rate"`           // 0.154 (US 배당소득세 15.4%)
	AfterTaxAmount float64    `json:"after_tax_amount"`   // total_amount × (1 − tax_rate)
	USDKRWRate     float64    `json:"usd_krw_rate"`       // exchange rate at time of entry
	AmountKRW      int64      `json:"amount_krw"`         // round(after_tax_amount × usd_krw_rate)
	ExDividendDate *time.Time `json:"ex_dividend_date,omitempty"` // 배당락일 (optional)
	PaymentDate    time.Time  `json:"payment_date"`       // 지급일
	IsApplied      bool       `json:"is_applied"`         // 가계부에 수입으로 반영됐는지
	Memo           string     `json:"memo"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

// DividendEventsFile mirrors the top-level structure of dividends.json.
type DividendEventsFile struct {
	Dividends []DividendEvent `json:"dividends"`
}

// CreateDividendRequest is the DTO for POST /api/dividends.
type CreateDividendRequest struct {
	UserID         string     `json:"user_id"`
	StockAssetID   string     `json:"stock_asset_id"`
	Symbol         string     `json:"symbol"`
	Exchange       string     `json:"exchange"`
	Name           string     `json:"name"`
	Quantity       float64    `json:"quantity"`
	AmountPerShare float64    `json:"amount_per_share"`
	Currency       string     `json:"currency"`
	TaxRate        float64    `json:"tax_rate"`    // defaults to 0.154
	USDKRWRate     float64    `json:"usd_krw_rate"`
	ExDividendDate *time.Time `json:"ex_dividend_date,omitempty"`
	PaymentDate    time.Time  `json:"payment_date"`
	Memo           string     `json:"memo"`
}

// DividendYearlySummary is the response for GET /api/dividends/summary.
type DividendYearlySummary struct {
	Year             int             `json:"year"`
	TotalUSD         float64         `json:"total_usd"`          // pre-tax
	TotalAfterTaxUSD float64         `json:"total_after_tax_usd"`
	TotalKRW         int64           `json:"total_krw"`
	AppliedCount     int             `json:"applied_count"`
	PendingCount     int             `json:"pending_count"`
	Events           []DividendEvent `json:"events"`
}

// NetWorthSummary is returned by GET /api/assets/net-worth.
// 주식 포트폴리오 + 기타 자산을 합산한 순자산 요약.
type NetWorthSummary struct {
	StockValueKRW float64   `json:"stock_value_krw"`
	AssetValueKRW int64     `json:"asset_value_krw"` // 기타 자산 합계
	LiabilityKRW  int64     `json:"liability_krw"`   // 부채 합계
	NetWorthKRW   float64   `json:"net_worth_krw"`   // 총합 − 부채
	CalculatedAt  time.Time `json:"calculated_at"`
}

// ─────────────────────────────────────────────
//  FixedExpense (고정비 — 정기 지출 템플릿)
// ─────────────────────────────────────────────

// RecurringCycle specifies how often the expense recurs.
type RecurringCycle string

const (
	RecurringMonthly RecurringCycle = "monthly" // 매월
	RecurringWeekly  RecurringCycle = "weekly"  // 매주
)

// FixedExpenseOwner identifies who is responsible for the expense.
type FixedExpenseOwner string

const (
	FixedOwnerHusband FixedExpenseOwner = "husband" // 남편
	FixedOwnerWife    FixedExpenseOwner = "wife"     // 아내
	FixedOwnerJoint   FixedExpenseOwner = "joint"    // 공동
)

// FixedExpenseKind distinguishes spending from saving fixed expenses.
type FixedExpenseKind string

const (
	FixedExpenseKindSpending FixedExpenseKind = "spending" // 소비성 (월세, 관리비 등)
	FixedExpenseKindSaving   FixedExpenseKind = "saving"   // 저축성 (정기 적금, 자동 주식매수 등)
)

// FixedExpense is a recurring expense template (e.g. rent, insurance, OTT).
// It is NOT a transaction itself — it is used to generate transactions each cycle.
type FixedExpense struct {
	ID         string            `json:"id"`
	CoupleID   string            `json:"couple_id"`
	UserID     string            `json:"user_id"`           // who registered it
	Owner      FixedExpenseOwner `json:"owner"`             // husband | wife | joint
	Kind       FixedExpenseKind  `json:"kind"`              // "spending" | "saving"
	Title      string            `json:"title"`             // e.g. "아파트 관리비"
	Category   string            `json:"category"`          // e.g. "주거비"
	Amount     int64             `json:"amount"`            // KRW
	Currency   string            `json:"currency"`          // 기본 "KRW"
	Cycle      RecurringCycle    `json:"cycle"`             // "monthly" | "weekly"
	DayOfMonth int               `json:"day_of_month"`      // 1–28, 매월 N일 이체 (cycle=monthly)
	DayOfWeek  *int              `json:"day_of_week,omitempty"` // 0=Sun…6=Sat (cycle=weekly)
	IsActive   bool              `json:"is_active"`
	Memo       string            `json:"memo"`
	SavingLink *SavingLink       `json:"saving_link,omitempty"` // non-nil when kind=="saving"
	CreatedAt  time.Time         `json:"created_at"`
	UpdatedAt  time.Time         `json:"updated_at"`
}

// FixedExpensesFile mirrors the top-level structure of fixed_expenses.json.
type FixedExpensesFile struct {
	FixedExpenses []FixedExpense `json:"fixed_expenses"`
}

// CreateFixedExpenseRequest is the DTO for POST /api/fixed-expenses.
type CreateFixedExpenseRequest struct {
	UserID     string            `json:"user_id"`
	Owner      FixedExpenseOwner `json:"owner"`
	Kind       FixedExpenseKind  `json:"kind"`       // default "spending"
	Title      string            `json:"title"`
	Category   string            `json:"category"`
	Amount     int64             `json:"amount"`
	Currency   string            `json:"currency"`
	Cycle      RecurringCycle    `json:"cycle"`
	DayOfMonth int               `json:"day_of_month"`
	DayOfWeek  *int              `json:"day_of_week,omitempty"`
	Memo       string            `json:"memo"`
	SavingLink *SavingLink       `json:"saving_link,omitempty"` // required when kind=="saving"
}

// UpdateFixedExpenseRequest is the DTO for PUT /api/fixed-expenses/{id}.
// All fields optional — only non-nil fields are applied.
type UpdateFixedExpenseRequest struct {
	Owner      *FixedExpenseOwner `json:"owner,omitempty"`
	Kind       *FixedExpenseKind  `json:"kind,omitempty"`
	Title      *string            `json:"title,omitempty"`
	Category   *string            `json:"category,omitempty"`
	Amount     *int64             `json:"amount,omitempty"`
	DayOfMonth *int               `json:"day_of_month,omitempty"`
	IsActive   *bool              `json:"is_active,omitempty"`
	Memo       *string            `json:"memo,omitempty"`
	SavingLink *SavingLink        `json:"saving_link,omitempty"`
}

// FixedExpenseSummary is the response for GET /api/fixed-expenses/summary.
type FixedExpenseSummary struct {
	TotalAmount  int64          `json:"total_amount"`  // sum of all active FE amounts
	AppliedCount int            `json:"applied_count"` // applied this month
	TotalCount   int            `json:"total_count"`   // total active FE count
	Unapplied    []FixedExpense `json:"unapplied"`     // not yet applied this month
}

// ApplyFixedExpensesResult is the response for POST /api/fixed-expenses/apply.
type ApplyFixedExpensesResult struct {
	Applied []Transaction `json:"applied"`
	Skipped int           `json:"skipped"` // already applied this month
	Total   int           `json:"total"`   // active FE count
}
