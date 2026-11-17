// Package store 负责 SQLite 的打开、建表与关闭。
// 使用 modernc.org/sqlite 纯 Go 驱动，免 cgo，可直接交叉编译到 Linux。
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // 注册驱动名 "sqlite"
)

type Store struct {
	DB *sql.DB
}

const schema = `
CREATE TABLE IF NOT EXISTS usage_logs (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	ts         INTEGER NOT NULL,              -- unix 秒
	key_name   TEXT    NOT NULL DEFAULT '',
	route      TEXT    NOT NULL DEFAULT '',
	target     TEXT    NOT NULL DEFAULT '',
	status     INTEGER NOT NULL DEFAULT 0,
	code       INTEGER NOT NULL DEFAULT 0,    -- 业务码，0 为成功
	latency_ms INTEGER NOT NULL DEFAULT 0,
	cache_hit  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_usage_ts ON usage_logs(ts);

CREATE TABLE IF NOT EXISTS scrape_cache (
	cache_key  TEXT PRIMARY KEY,
	payload    TEXT    NOT NULL,
	expires_at INTEGER NOT NULL       -- unix 毫秒
);
`

// Open 打开（必要时创建）SQLite 数据库并执行建表语句。
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("创建数据目录: %w", err)
		}
	}
	// WAL + busy_timeout + 页缓存：混部小机器上读写更稳
	dsn := path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=cache_size(-4000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite: %w", err)
	}
	// 单连接串行化，规避 SQLITE_BUSY；本服务流量小，单连接足够
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("建表: %w", err)
	}
	return &Store{DB: db}, nil
}

func (s *Store) Close() error { return s.DB.Close() }
