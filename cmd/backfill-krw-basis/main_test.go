package main

import (
	"math"
	"strings"
	"testing"
)

func TestPropose_WeightsTheRateByDollarAmount(t *testing.T) {
	// 10주 @ $100 / 1,300 + 10주 @ $200 / 1,400 → 가중환율 1,366.67, 달러 평단 150 → 205,000원
	p := propose(150, []buyLeg{{10, 100, 1300}, {10, 200, 1400}})
	if !p.OK || math.Abs(p.Value-205_000) > 0.01 {
		t.Errorf("want (205000, OK), got (%v, %v) %s", p.Value, p.OK, p.Source)
	}
}

func TestPropose_IgnoresBuysWithoutARateAndSaysSo(t *testing.T) {
	p := propose(150, []buyLeg{{10, 100, 0}, {10, 200, 1400}})
	if !p.OK || math.Abs(p.Value-210_000) > 0.01 {
		t.Errorf("want (210000, OK), got (%v, %v)", p.Value, p.OK)
	}
	if !strings.Contains(p.Source, "10.0000주 / 전체 매수 20.0000주") {
		t.Errorf("환율 기록 비율을 보여줘야 한다: %s", p.Source)
	}
}

func TestPropose_DoesNotGuessWithoutAnyRate(t *testing.T) {
	p := propose(150, []buyLeg{{10, 100, 0}})
	if p.OK {
		t.Errorf("환율 기록이 없으면 수동이어야 한다: %+v", p)
	}
	if p := propose(150, nil); p.OK {
		t.Errorf("매수 기록이 없으면 수동이어야 한다: %+v", p)
	}
}

func TestPropose_RejectsAnImplausibleResult(t *testing.T) {
	// 환율 9.4 로 잘못 기록된 매수만 있으면 결과도 비정상 → 수동
	if p := propose(334.41, []buyLeg{{329, 334.41, 9.4}}); p.OK {
		t.Errorf("비정상 결과를 제안했다: %+v", p)
	}
}
