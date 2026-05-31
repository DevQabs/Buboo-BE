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
		catRepo      repository.CategoryRepository
	)

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
	catRepo      = repository.NewPgCategoryRepository(pool)

	// ── S3-compatible Storage ─────────────────────────────────────────────────
	var stor *storage.SupabaseStorage
	s3Endpoint  := os.Getenv("S3_ENDPOINT")
	s3AccessKey := os.Getenv("S3_ACCESS_KEY_ID")
	s3SecretKey := os.Getenv("S3_SECRET_ACCESS_KEY")
	s3Bucket    := envOr("S3_BUCKET", "diary-photos")
	s3PublicBase := os.Getenv("S3_PUBLIC_BASE")
	if s3Endpoint != "" && s3AccessKey != "" && s3SecretKey != "" {
		stor = storage.New(s3Endpoint, s3AccessKey, s3SecretKey, s3Bucket, s3PublicBase)
		log.Println("✅ S3 Storage 연결 성공")
	} else {
		log.Println("⚠️  S3 Storage 미설정 — 로컬 파일 업로드 사용")
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
		feRepo, divRepo, scheduleRepo, diaryRepo, catRepo,
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
