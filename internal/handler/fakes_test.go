package handler

import (
	"context"
	"fmt"

	"github.com/yourname/couple-app/internal/models"
	"github.com/yourname/couple-app/internal/repository"
)

// 인터페이스를 내장해, 테스트가 쓰는 메서드만 구현한다. 안 쓴 메서드를 부르면 nil 패닉으로 드러난다.
type fakeTxRepo struct {
	repository.TransactionRepository
	txs    map[string]*models.Transaction
	delErr error
}

func (f *fakeTxRepo) GetByID(_ context.Context, id string) (*models.Transaction, error) {
	if tx, ok := f.txs[id]; ok {
		return tx, nil
	}
	return nil, fmt.Errorf("transaction %s not found", id)
}

func (f *fakeTxRepo) Delete(_ context.Context, id string) error {
	if f.delErr != nil {
		return f.delErr
	}
	delete(f.txs, id)
	return nil
}

type fakeDivRepo struct {
	repository.DividendRepository
	divs map[string]*models.DividendEvent
}

func (f *fakeDivRepo) GetByID(_ context.Context, id string) (*models.DividendEvent, error) {
	if d, ok := f.divs[id]; ok {
		return d, nil
	}
	return nil, fmt.Errorf("dividend %s not found", id)
}

func (f *fakeDivRepo) Delete(_ context.Context, id string) error {
	delete(f.divs, id)
	return nil
}
