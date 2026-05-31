package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yourname/couple-app/internal/db"
	"github.com/yourname/couple-app/internal/handler"
	"github.com/yourname/couple-app/internal/repository"
	"github.com/yourname/couple-app/internal/service"
	"github.com/yourname/couple-app/internal/storage"
)

func main() {
	port     := envOr("PORT", "8090")
	dataDir  := envOr("DATA_DIR", "./data")
	coupleID := envOr("COUPLE_ID", "couple-001")
	allowedOrigins := parseOrigins(envOr("ALLOWED_ORIGINS", "*"))

	ctx := context.Background()

	// ── Repositories ─────────────────────────────────────────────────────────
	var (
		txRepo       repository.TransactionRepository
		stockRepo    repository.StockRepository
		userRepo     repository.UserRepository
		assetRepo    repository.OtherAssetRepository
		stxRepo      repository.StockTransactionRepository
		feRepo       repository.FixedExpenseRepository
		divRepo      repository.DividendRepository
		scheduleRepo repository.ScheduleRepository
		diaryRepo    repository.DiaryRepository
	)

	if os.Getenv("DATABASE_URL") != "" {
		pool, err := db.New(ctx)
		if err != nil {
			log.Fatalf("DB 연결 실패: %v", err)
		}
		log.Println("✅ PostgreSQL 연결 성공")
		txRepo       = repository.NewPgTransactionRepository(pool)
		stockRepo    = repository.NewPgStockRepository(pool)
		userRepo     = repository.NewPgUserRepository(pool)
		assetRepo    = repository.NewPgOtherAssetRepository(pool)
		stxRepo      = repository.NewPgStockTransactionRepository(pool)
		feRepo       = repository.NewPgFixedExpenseRepository(pool)
		divRepo      = repository.NewPgDividendRepository(pool)
		scheduleRepo = repository.NewPgScheduleRepository(pool)
		diaryRepo    = repository.NewPgDiaryRepository(pool)
	} else {
		log.Println("⚠️  DATABASE_URL 미설정 — 파일 기반 저장소 사용")
		txRepo       = repository.NewFileTransactionRepository(filepath.Join(dataDir, "transactions.json"))
		stockRepo    = repository.NewFileStockRepository(filepath.Join(dataDir, "stocks.json"))
		userRepo     = repository.NewFileUserRepository(filepath.Join(dataDir, "users.json"))
		assetRepo    = repository.NewFileOtherAssetRepository(filepath.Join(dataDir, "other_assets.json"))
		stxRepo      = repository.NewFileStockTransactionRepository(filepath.Join(dataDir, "stock_transactions.json"))
		feRepo       = repository.NewFileFixedExpenseRepository(filepath.Join(dataDir, "fixed_expenses.json"))
		divRepo      = repository.NewFileDividendRepository(filepath.Join(dataDir, "dividends.json"))
		scheduleRepo = repository.NewFileScheduleRepository(filepath.Join(dataDir, "schedules.json"))
		diaryRepo    = repository.NewFileDiaryRepository(filepath.Join(dataDir, "diaries.json"))
	}

	// ── Supabase Storage ──────────────────────────────────────────────────────
	var stor *storage.SupabaseStorage
	projectURL := envOr("SUPABASE_PROJECT_URL", os.Getenv("NEXT_PUBLIC_SUPABASE_URL"))
	serviceKey := os.Getenv("SUPABASE_SERVICE_KEY")
	if projectURL != "" && serviceKey != "" {
		stor = storage.New(projectURL, serviceKey)
		log.Println("✅ Supabase Storage 연결 성공")
	} else {
		log.Println("⚠️  Supabase Storage 미설정 — 로컬 파일 업로드 사용")
	}

	uploadsDir := filepath.Join(dataDir, "uploads")
	if stor == nil {
		if err := os.MkdirAll(uploadsDir, 0755); err != nil {
			log.Fatalf("uploads 디렉토리 생성 실패: %v", err)
		}
	}

	// ── Services ──────────────────────────────────────────────────────────────
	priceSvc  := service.NewPriceService()
	savingSvc := service.NewSavingService(txRepo, stockRepo, stxRepo, assetRepo, coupleID)

	// ── Router ────────────────────────────────────────────────────────────────
	r := handler.New(
		txRepo, stockRepo, stxRepo, userRepo, assetRepo,
		feRepo, divRepo, scheduleRepo, diaryRepo,
		priceSvc, savingSvc, coupleID, allowedOrigins, uploadsDir, stor,
	).NewRouter()

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		fmt.Printf("✅  서버 시작: http://0.0.0.0:%s\n", port)
		fmt.Printf("📂  데이터 경로: %s\n", dataDir)
		fmt.Printf("🌐  허용 Origin: %s\n", strings.Join(allowedOrigins, ", "))
		fmt.Println("   Ctrl+C 로 종료")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("서버 오류: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("서버 종료 중...")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		log.Fatalf("강제 종료: %v", err)
	}
	log.Println("서버 정상 종료")
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseOrigins(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if s := strings.TrimSpace(p); s != "" {
			out = append(out, s)
		}
	}
	return out
}
