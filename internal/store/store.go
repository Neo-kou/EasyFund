package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"github.com/neokou/easyfund/internal/model"
)

// Store SQLite 存储(modernc 纯 Go 驱动, WAL 模式, 读写分离留给未来 serve 并发)。
type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	const ddl = `
CREATE TABLE IF NOT EXISTS fund (
  code       TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  index_name TEXT NOT NULL,
  share      TEXT NOT NULL DEFAULT '',
  etf_code   TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS snapshot (
  code            TEXT NOT NULL,
  trade_date      TEXT NOT NULL,
  purchase_status TEXT NOT NULL,
  status_raw      TEXT NOT NULL DEFAULT '',
  daily_limit     REAL,
  min_buy         REAL,
  purchase_fee    REAL,
  nav             REAL,
  nav_date        TEXT NOT NULL DEFAULT '',
  etf_code        TEXT NOT NULL DEFAULT '',
  etf_price       REAL,
  etf_premium     REAL,
  fetched_at      TEXT NOT NULL,
  PRIMARY KEY (code, trade_date)
);
CREATE INDEX IF NOT EXISTS idx_snapshot_date ON snapshot(trade_date);
`
	_, err := s.db.Exec(ddl)
	return err
}

// UpsertFunds config 基金池 → fund 表(运行时副本, 每次 crawl 刷新)。
func (s *Store) UpsertFunds(funds []model.Fund) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const q = `INSERT INTO fund(code,name,index_name,share,etf_code) VALUES(?,?,?,?,?)
ON CONFLICT(code) DO UPDATE SET name=excluded.name, index_name=excluded.index_name, share=excluded.share, etf_code=excluded.etf_code`
	for _, f := range funds {
		if _, err := tx.Exec(q, f.Code, f.Name, f.Index, f.Share, f.EtfCode); err != nil {
			return fmt.Errorf("fund %s: %w", f.Code, err)
		}
	}
	return tx.Commit()
}

// UpsertSnapshots 快照批量 UPSERT, (code, trade_date) 主键保证 cron 重跑幂等。
func (s *Store) UpsertSnapshots(snaps []model.Snapshot) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	const q = `INSERT INTO snapshot(code,trade_date,purchase_status,status_raw,daily_limit,min_buy,purchase_fee,nav,nav_date,etf_code,etf_price,etf_premium,fetched_at)
VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(code,trade_date) DO UPDATE SET
  purchase_status=excluded.purchase_status, status_raw=excluded.status_raw,
  daily_limit=excluded.daily_limit, min_buy=excluded.min_buy, purchase_fee=excluded.purchase_fee,
  nav=excluded.nav, nav_date=excluded.nav_date, etf_code=excluded.etf_code,
  etf_price=excluded.etf_price, etf_premium=excluded.etf_premium, fetched_at=excluded.fetched_at`
	for i := range snaps {
		sn := snaps[i]
		if _, err := tx.Exec(q,
			sn.Code, sn.TradeDate, string(sn.PurchaseStatus), sn.StatusRaw,
			nullF(sn.DailyLimit), nullF(sn.MinBuy), nullF(sn.PurchaseFee),
			nullF(sn.Nav), sn.NavDate, sn.EtfCode,
			nullF(sn.EtfPrice), nullF(sn.EtfPremium), sn.FetchedAt,
		); err != nil {
			return fmt.Errorf("snapshot %s@%s: %w", sn.Code, sn.TradeDate, err)
		}
	}
	return tx.Commit()
}

// PrevSnapshots trade_date 严格早于 curDate 的最近一个交易日的全量快照; 无历史时返回 nil。
func (s *Store) PrevSnapshots(curDate string) (map[string]model.Snapshot, error) {
	var prevDate string
	err := s.db.QueryRow(`SELECT MAX(trade_date) FROM snapshot WHERE trade_date < ?`, curDate).Scan(&prevDate)
	if err == sql.ErrNoRows || prevDate == "" {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.snapshotsAt(prevDate)
}

// LatestSnapshots 最新一个交易日的全量快照(serve/API 用)。
func (s *Store) LatestSnapshots() (map[string]model.Snapshot, string, error) {
	var date string
	err := s.db.QueryRow(`SELECT MAX(trade_date) FROM snapshot`).Scan(&date)
	if err == sql.ErrNoRows || date == "" {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	snaps, err := s.snapshotsAt(date)
	return snaps, date, err
}

func (s *Store) snapshotsAt(date string) (map[string]model.Snapshot, error) {
	rows, err := s.db.Query(`SELECT code,trade_date,purchase_status,status_raw,daily_limit,min_buy,purchase_fee,nav,nav_date,etf_code,etf_price,etf_premium,fetched_at
FROM snapshot WHERE trade_date=?`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]model.Snapshot{}
	for rows.Next() {
		var sn model.Snapshot
		var limit, minBuy, fee, nav, price, premium sql.NullFloat64
		if err := rows.Scan(&sn.Code, &sn.TradeDate, &sn.PurchaseStatus, &sn.StatusRaw,
			&limit, &minBuy, &fee, &nav, &sn.NavDate, &sn.EtfCode, &price, &premium, &sn.FetchedAt); err != nil {
			return nil, err
		}
		sn.PurchaseStatus = model.Status(sn.PurchaseStatus)
		sn.DailyLimit, sn.MinBuy, sn.PurchaseFee = ptr(limit), ptr(minBuy), ptr(fee)
		sn.Nav, sn.EtfPrice, sn.EtfPremium = ptr(nav), ptr(price), ptr(premium)
		out[sn.Code] = sn
	}
	return out, rows.Err()
}

func nullF(p *float64) sql.NullFloat64 {
	if p == nil {
		return sql.NullFloat64{}
	}
	return sql.NullFloat64{Float64: *p, Valid: true}
}

func ptr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}
