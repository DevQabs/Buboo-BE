// Command migrate-storage copies diary photos from the old Supabase bucket to
// the R2 bucket and rewrites diaries.photos from public URLs to object keys,
// recording each object's size in storage_objects (설계서 5절).
//
// Dry-run unless -apply. Safe to re-run: keys already in place are skipped and
// a copy always targets the same key. It never deletes Supabase originals —
// the user removes them after checking.
//
//	go run ./cmd/migrate-storage          # 미리보기
//	go run ./cmd/migrate-storage -apply   # 실행 (사용자 승인 후)
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"mime"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/yourname/couple-app/internal/db"
	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
	"github.com/yourname/couple-app/internal/storage"
)

type step struct {
	kind string // copy | key | skip
	src  string // Supabase 객체 이름 (copy)
	key  string // R2 키 (copy, key)
}

// plan decides what to do with one diaries.photos entry.
func plan(photo, legacyPrefix, coupleID string) step {
	switch {
	case strings.HasPrefix(photo, legacyPrefix):
		name := strings.TrimPrefix(photo, legacyPrefix)
		return step{kind: "copy", src: name, key: coupleID + "/diary/" + path.Base(name)}
	case strings.HasPrefix(photo, coupleID+"/diary/"):
		return step{kind: "key", key: photo}
	default:
		return step{kind: "skip"}
	}
}

type item struct {
	step
	diaryID, coupleID, photo string
	size                     int64
	record                   bool // key 인데 storage_objects 행이 없음
}

func main() {
	apply := flag.Bool("apply", false, "실제로 복사하고 DB 를 고친다 (없으면 미리보기만)")
	flag.Parse()
	loadDotEnv()
	ctx := context.Background()

	srcBucket := envOr("S3_BUCKET", "diary-photos")
	legacyPrefix := strings.TrimRight(need("S3_PUBLIC_BASE"), "/") + "/" + srcBucket + "/"
	src := storage.NewClient(storage.Config{
		Endpoint: need("S3_ENDPOINT"), AccessKeyID: need("S3_ACCESS_KEY_ID"), SecretAccessKey: need("S3_SECRET_ACCESS_KEY"),
		Bucket: srcBucket, Region: envOr("S3_REGION", "ap-southeast-2"), // 옛 코드가 쓰던 값
	})
	dst := storage.NewClient(storage.Config{
		Endpoint: need("R2_ENDPOINT"), AccessKeyID: need("R2_ACCESS_KEY_ID"), SecretAccessKey: need("R2_SECRET_ACCESS_KEY"),
		Bucket: envOr("R2_BUCKET", "uri-jip-media"), Region: envOr("R2_REGION", "auto"),
	})
	limit, err := strconv.ParseInt(envOr("STORAGE_LIMIT_BYTES", "9500000000"), 10, 64)
	if err != nil {
		log.Fatalf("STORAGE_LIMIT_BYTES: %v", err)
	}

	pool, err := db.New(ctx)
	if err != nil {
		log.Fatalf("DB 연결 실패: %v", err)
	}
	defer pool.Close()
	store := repository.NewPgStorageRepository(pool)

	rows, err := pool.Query(ctx, `SELECT id, couple_id, photos FROM diaries WHERE cardinality(photos) > 0 ORDER BY id`)
	if err != nil {
		log.Fatalf("일기 조회 실패: %v", err)
	}
	var items []item
	for rows.Next() {
		var id, couple string
		var photos []string
		if err := rows.Scan(&id, &couple, &photos); err != nil {
			log.Fatalf("일기 읽기 실패: %v", err)
		}
		for _, p := range photos {
			items = append(items, item{step: plan(p, legacyPrefix, couple), diaryID: id, coupleID: couple, photo: p})
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		log.Fatalf("일기 조회 실패: %v", err)
	}

	// 1) 미리보기: 크기 확인
	var copyN, copyBytes, keyN, recordN, skipN int64
	for i := range items {
		it := &items[i]
		switch it.kind {
		case "copy":
			if it.size, err = src.Head(ctx, it.src); err != nil {
				log.Fatalf("원본 크기 확인 실패 (일기 %s, %s): %v", it.diaryID, it.photo, err)
			}
			copyN++
			copyBytes += it.size
		case "key":
			keyN++
			o, err := store.Get(ctx, it.key)
			if err != nil {
				log.Fatalf("사용량 기록 조회 실패: %v", err)
			}
			if o == nil {
				if it.size, err = dst.Head(ctx, it.key); err != nil {
					log.Fatalf("R2 객체 확인 실패 (%s): %v", it.key, err)
				}
				it.record = true
				recordN++
			}
		default:
			skipN++
			fmt.Printf("건너뜀(알 수 없는 값): 일기 %s  %s\n", it.diaryID, it.photo)
		}
	}
	used, err := store.TotalBytes(ctx)
	if err != nil {
		log.Fatalf("사용량 합계 실패: %v", err)
	}
	fmt.Printf("복사할 사진 %d개 (%d 바이트), 이미 키 %d개 (기록 보충 %d개), 건너뜀 %d개\n", copyN, copyBytes, keyN, recordN, skipN)
	fmt.Printf("저장 공간: 지금 %d + 복사 %d = %d / 상한 %d 바이트\n", used, copyBytes, used+copyBytes, limit)
	if used+copyBytes > limit {
		fmt.Println("상한을 넘습니다. 사용자 확인 전에는 진행하지 않습니다.")
		os.Exit(1)
	}
	if !*apply {
		fmt.Println("미리보기만 했습니다. 실제로 하려면 -apply 를 붙이세요.")
		return
	}

	// 2) 실행: 복사 → 기록 → DB 값 교체 (어디서 멈춰도 다시 돌리면 이어진다)
	for _, it := range items {
		switch {
		case it.kind == "copy":
			data, ct, err := src.Get(ctx, it.src)
			if err != nil {
				log.Fatalf("원본 읽기 실패 (%s): %v", it.photo, err)
			}
			if ct == "" {
				ct = mime.TypeByExtension(path.Ext(it.key))
			}
			if err := dst.Put(ctx, it.key, data, ct); err != nil {
				log.Fatalf("R2 쓰기 실패 (%s): %v", it.key, err)
			}
			if err := store.Record(ctx, models.StorageObject{Key: it.key, CoupleID: it.coupleID, Purpose: "diary", SizeBytes: int64(len(data))}); err != nil {
				log.Fatalf("사용량 기록 실패 (%s): %v", it.key, err)
			}
			if _, err := pool.Exec(ctx,
				`UPDATE diaries SET photos = array_replace(photos, $2, $3), updated_at = NOW() WHERE id = $1`,
				it.diaryID, it.photo, it.key); err != nil {
				log.Fatalf("일기 갱신 실패 (%s): %v", it.diaryID, err)
			}
			fmt.Printf("복사: %s -> %s\n", it.photo, it.key)
		case it.record:
			if err := store.Record(ctx, models.StorageObject{Key: it.key, CoupleID: it.coupleID, Purpose: "diary", SizeBytes: it.size}); err != nil {
				log.Fatalf("사용량 기록 실패 (%s): %v", it.key, err)
			}
			fmt.Printf("기록 보충: %s (%d 바이트)\n", it.key, it.size)
		}
	}
	fmt.Println("완료. Supabase 원본은 지우지 않았습니다.")
}

func need(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("%s 환경변수가 필요합니다", k)
	}
	return v
}

func envOr(k, fallback string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return fallback
}

// loadDotEnv copies cmd/main.go's loader (main package can't be imported).
// ponytail: 두 벌 — 세 번째 명령이 생기면 internal 패키지로 옮긴다.
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
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
}
