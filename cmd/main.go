// main.go — 서버 진입점
//
// 실행:
//   go run ./cmd/main.go
//   PORT=9090 go run ./cmd/main.go                         (포트 변경)
//   ALLOWED_ORIGINS=http://100.x.x.x:3300 go run ./cmd/main.go  (Tailscale 등 외부 origin 허용)
//
// ALLOWED_ORIGINS 기본값: * (모든 origin 허용)
// 쉼표로 복수 지정 가능: "http://localhost:3300,http://100.64.0.2:3300"
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

	"github.com/yourname/couple-app/internal/handler"
	"github.com/yourname/couple-app/internal/repository"
	"github.com/yourname/couple-app/internal/service"
)

func main() {
	// ── 설정 ──────────────────────────────────────────────────────────────────
	port     := envOr("PORT", "8090")
	dataDir  := envOr("DATA_DIR", "./data")
	coupleID := envOr("COUPLE_ID", "couple-001")

	// ALLOWED_ORIGINS: 쉼표 구분 목록. 기본값 "*" (개발용 전체 허용)
	// Tailscale 사용 시 별도 설정 불필요 — 서버는 0.0.0.0 바인딩이므로
	// Tailscale IP로 접근 가능. 프론트엔드 origin이 다를 경우만 명시 설정.
	allowedOrigins := parseOrigins(envOr("ALLOWED_ORIGINS", "*"))

	// ── Repository 주입 ──────────────────────────────────────────────────────
	txRepo := repository.NewFileTransactionRepository(
		filepath.Join(dataDir, "transactions.json"),
	)
	stockRepo := repository.NewFileStockRepository(
		filepath.Join(dataDir, "stocks.json"),
	)
	userRepo := repository.NewFileUserRepository(
		filepath.Join(dataDir, "users.json"),
	)
	assetRepo := repository.NewFileOtherAssetRepository(
		filepath.Join(dataDir, "other_assets.json"),
	)
	stxRepo := repository.NewFileStockTransactionRepository(
		filepath.Join(dataDir, "stock_transactions.json"),
	)
	feRepo := repository.NewFileFixedExpenseRepository(
		filepath.Join(dataDir, "fixed_expenses.json"),
	)
	divRepo := repository.NewFileDividendRepository(
		filepath.Join(dataDir, "dividends.json"),
	)
	scheduleRepo := repository.NewFileScheduleRepository(
		filepath.Join(dataDir, "schedules.json"),
	)
	uploadsDir := filepath.Join(dataDir, "uploads")
	if err := os.MkdirAll(uploadsDir, 0755); err != nil {
		log.Fatalf("uploads 디렉토리 생성 실패: %v", err)
	}
	diaryRepo := repository.NewFileDiaryRepository(
		filepath.Join(dataDir, "diaries.json"),
	)

	// ── Price Service ─────────────────────────────────────────────────────────
	priceSvc := service.NewPriceService()

	// ── Saving Service ────────────────────────────────────────────────────────
	savingSvc := service.NewSavingService(txRepo, stockRepo, stxRepo, assetRepo, coupleID)

	// ── Router ───────────────────────────────────────────────────────────────
	r := handler.New(txRepo, stockRepo, stxRepo, userRepo, assetRepo, feRepo, divRepo, scheduleRepo, diaryRepo, priceSvc, savingSvc, coupleID, allowedOrigins, uploadsDir).NewRouter()

	// ── HTTP 서버 — 0.0.0.0 바인딩으로 Tailscale 포함 모든 인터페이스 수신 ────
	srv := &http.Server{
		Addr:         ":" + port, // ":" 접두사 = 0.0.0.0 (모든 인터페이스)
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

	// ── Graceful shutdown ─────────────────────────────────────────────────────
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("서버 종료 중...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
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

// parseOrigins splits a comma-separated origin list and trims whitespace.
// A single "*" is returned as-is for CORS wildcard.
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
