package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
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
	loadDotEnv()
	port := envOr("PORT", "8090")
	dataDir := envOr("DATA_DIR", "./data")
	allowedOrigins := parseOrigins(envOr("ALLOWED_ORIGINS", "*"))

	jwtSecret := []byte(os.Getenv("JWT_SECRET"))
	if len(jwtSecret) == 0 {
		log.Fatal("JWT_SECRET is required")
	}

	internalSecret := []byte(os.Getenv("INTERNAL_API_SECRET"))
	if len(internalSecret) == 0 {
		log.Fatal("INTERNAL_API_SECRET 환경변수가 필요합니다 (FE 서버와 같은 값)")
	}

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
		inviteRepo   repository.InviteRepository
		roadmapRepo  repository.RoadmapRepository
		ownerRepo    *repository.PgOwnership
	)

	pool, err := db.New(ctx)
	if err != nil {
		log.Fatalf("DB 연결 실패: %v", err)
	}
	log.Println("✅ PostgreSQL 연결 성공")
	txRepo = repository.NewPgTransactionRepository(pool)
	stockRepo = repository.NewPgStockRepository(pool)
	userRepo = repository.NewPgUserRepository(pool)
	assetRepo = repository.NewPgOtherAssetRepository(pool)
	stxRepo = repository.NewPgStockTransactionRepository(pool)
	feRepo = repository.NewPgFixedExpenseRepository(pool)
	divRepo = repository.NewPgDividendRepository(pool)
	scheduleRepo = repository.NewPgScheduleRepository(pool)
	diaryRepo = repository.NewPgDiaryRepository(pool)
	catRepo = repository.NewPgCategoryRepository(pool)
	inviteRepo = repository.NewPgInviteRepository(pool)
	roadmapRepo = repository.NewPgRoadmapRepository(pool)
	ownerRepo = repository.NewPgOwnership(pool)
	permRepo := repository.NewPgPermissionRepository(pool)
	storageRepo := repository.NewPgStorageRepository(pool)
	babyRepo := repository.NewPgBabyRepository(pool)
	pushRepo := repository.NewPgPushRepository(pool)

	// ── 미디어 저장소 (R2 등 S3 호환, 설계서 5절) ──────────────────────────────
	var media *storage.Client
	if os.Getenv("R2_ENDPOINT") != "" && os.Getenv("R2_ACCESS_KEY_ID") != "" && os.Getenv("R2_SECRET_ACCESS_KEY") != "" {
		media = storage.NewClient(storage.Config{
			Endpoint:        os.Getenv("R2_ENDPOINT"),
			AccessKeyID:     os.Getenv("R2_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("R2_SECRET_ACCESS_KEY"),
			Bucket:          envOr("R2_BUCKET", "uri-jip-media"),
			Region:          envOr("R2_REGION", "auto"),
		})
		log.Println("미디어 저장소(R2) 설정됨")
	} else {
		log.Println("R2_* 미설정 — 사진·영상 업로드 불가(503)")
	}
	storageLimit, err := strconv.ParseInt(envOr("STORAGE_LIMIT_BYTES", "9500000000"), 10, 64)
	if err != nil || storageLimit <= 0 {
		log.Fatalf("STORAGE_LIMIT_BYTES 는 양의 정수(바이트)여야 합니다: %q", os.Getenv("STORAGE_LIMIT_BYTES"))
	}

	uploadsDir := filepath.Join(dataDir, "uploads") // 이전 전 로컬 사진 표시용(읽기 전용)

	// ── 웹 푸시 (push 설계서 6절) — VAPID 미설정이면 푸시만 끈다 ──────────────
	vapidPublic := os.Getenv("VAPID_PUBLIC_KEY")
	pusher := handler.NewWebPush(vapidPublic, os.Getenv("VAPID_PRIVATE_KEY"), os.Getenv("VAPID_SUBJECT"))
	if pusher == nil {
		log.Println("VAPID_* 미설정 — 웹 푸시 꺼짐(구독 API 503, run 은 아무것도 안 함)")
	}

	// ── Services ──────────────────────────────────────────────────────────────
	priceSvc := service.NewPriceService()
	savingSvc := service.NewSavingService(txRepo, stockRepo, stxRepo, assetRepo, priceSvc.FetchUSDKRW)

	// ── Router ────────────────────────────────────────────────────────────────
	r := handler.New(
		txRepo, stockRepo, stxRepo, userRepo, assetRepo,
		feRepo, divRepo, scheduleRepo, diaryRepo, catRepo,
		inviteRepo, roadmapRepo, priceSvc, savingSvc, jwtSecret, allowedOrigins, uploadsDir, ownerRepo, internalSecret, permRepo, media, storageRepo, storageLimit, babyRepo, pushRepo, pusher, vapidPublic, []byte(os.Getenv("PUSH_CRON_SECRET")),
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

func loadDotEnv() {
	data, err := os.ReadFile(".env")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
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
