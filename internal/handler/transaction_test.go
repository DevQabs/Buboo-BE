package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/yourname/couple-app/internal/models"
)

func TestMergeTransaction_빠진필드는그대로(t *testing.T) {
	day := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	fe := "fe-1"
	old := &models.Transaction{ID: "t1", CoupleID: "c1", Type: "expense", Amount: 5000,
		Category: "식비", Date: day, Tags: []string{"점심"}, FixedExpenseID: &fe,
		SavingLink: &models.SavingLink{Kind: models.SavingKindDeposit, LinkAssetID: "a1"}}

	got, err := mergeTransaction(old, strings.NewReader(`{"title":"김밥","id":"x","couple_id":"other","saving_link":{"kind":"deposit","link_asset_id":"a2"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if got.Title != "김밥" || got.Amount != 5000 || got.Category != "식비" || !got.Date.Equal(day) ||
		got.FixedExpenseID == nil || len(got.Tags) != 1 {
		t.Fatalf("보내지 않은 필드가 지워졌다: %+v", got)
	}
	if got.ID != "t1" || got.CoupleID != "c1" {
		t.Fatal("id·couple_id 는 본문으로 바꿀 수 없다")
	}
	if old.Title != "" || old.SavingLink.LinkAssetID != "a1" {
		t.Fatal("기존 거래 값이 함께 바뀌었다")
	}
}
