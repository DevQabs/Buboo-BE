-- 우리집 5단계(돈·세금 버그) 마이그레이션.
-- Supabase SQL Editor 에서 번호 순서대로 한 덩어리씩 실행한다. 여러 번 실행해도 결과가 같다.
-- SELECT 로 시작하는 블록은 미리 보기다. 결과를 확인한 뒤 다음 블록으로 간다.

-- 1. 매매 기록 출처. 'trade' = 매수·매도 화면, 'saving' = 저축 거래가 대신 산 매수.
--    로드맵 적립은 저축 거래 금액으로 이미 세므로 'saving' 기록은 다시 세지 않는다.
ALTER TABLE stock_transactions ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'trade';

-- 1-1. (미리 보기) 기존 저축 경유 매수 기록. 저축 거래와 연결 종목·시각이 같은 매수다.
SELECT s.id, s.symbol, s.quantity, s.price, s.executed_at, t.id AS saving_tx_id, t.amount
  FROM stock_transactions s
  JOIN transactions t
    ON t.couple_id = s.couple_id AND t.type = 'saving'
   AND t.saving_link->>'link_asset_id' = s.stock_asset_id
   AND t.date = s.executed_at
 WHERE s.type = 'buy' AND s.source = 'trade';

-- 1-2. 위 목록이 저축으로 산 매수가 맞으면 실행한다.
UPDATE stock_transactions s SET source = 'saving'
  FROM transactions t
 WHERE t.couple_id = s.couple_id AND t.type = 'saving'
   AND t.saving_link->>'link_asset_id' = s.stock_asset_id
   AND t.date = s.executed_at
   AND s.type = 'buy' AND s.source = 'trade';

-- 2. 배당이 만든 가계부 수입 거래. 배당을 지우면 이 거래도 지운다.
--    이 컬럼 전에 만든 배당은 연결이 없어, 지워도 수입 거래가 남는다(종전과 같음).
ALTER TABLE dividends ADD COLUMN IF NOT EXISTS transaction_id TEXT NOT NULL DEFAULT '';

-- 3-1. (미리 보기) 같은 고정비가 같은 날 두 번 반영된 행. 결과가 있으면 사용자와
--      지울 행(ids 중 하나)을 정해 지운 뒤 3-2 를 실행한다. 있으면 3-2 가 실패한다.
SELECT fixed_expense_id, date, COUNT(*) AS n, array_agg(id ORDER BY created_at) AS ids
  FROM transactions
 WHERE fixed_expense_id IS NOT NULL AND fixed_expense_id <> ''
 GROUP BY fixed_expense_id, date
HAVING COUNT(*) > 1;

-- 3-2. 고정비 반영 중복 방지.
CREATE UNIQUE INDEX IF NOT EXISTS uq_transactions_fixed_expense_date
    ON transactions(fixed_expense_id, date)
 WHERE fixed_expense_id IS NOT NULL AND fixed_expense_id <> '';
