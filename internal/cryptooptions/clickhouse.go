package cryptooptions

import (
"context"
"fmt"
"log"
"os"
"strings"

"github.com/ClickHouse/clickhouse-go/v2"
"github.com/ClickHouse/clickhouse-go/v2/lib/driver"
)

// InitSchema reads the DDL file and executes each statement against ClickHouse.
func InitSchema(ctx context.Context, conn driver.Conn, ddlPath string) error {
data, err := os.ReadFile(ddlPath)
if err != nil {
return fmt.Errorf("read DDL file %s: %w", ddlPath, err)
}

statements := strings.Split(string(data), ";")
for _, stmt := range statements {
stmt = strings.TrimSpace(stmt)
if stmt == "" || strings.HasPrefix(stmt, "--") {
continue
}
if err := conn.Exec(ctx, stmt); err != nil {
return fmt.Errorf("exec DDL: %w\nStatement: %s", err, stmt)
}
}
return nil
}

// ConnectClickHouse establishes a connection to ClickHouse.
func ConnectClickHouse(ctx context.Context, dsn string) (driver.Conn, error) {
opts, err := clickhouse.ParseDSN(dsn)
if err != nil {
return nil, fmt.Errorf("parse ClickHouse DSN: %w", err)
}
conn, err := clickhouse.Open(opts)
if err != nil {
return nil, fmt.Errorf("open ClickHouse connection: %w", err)
}
if err := conn.Ping(ctx); err != nil {
return nil, fmt.Errorf("ping ClickHouse: %w", err)
}
return conn, nil
}

// InsertSymbols batch-inserts symbol metadata into crypto_options_symbol_meta.
func InsertSymbols(ctx context.Context, conn driver.Conn, symbols []SymbolMeta) error {
if len(symbols) == 0 {
return nil
}

batch, err := conn.PrepareBatch(ctx, `INSERT INTO crypto_options_symbol_meta (
symbol_id, symbol, base_asset, option_type, strike_price, expiration, underlying_index
)`)
if err != nil {
return fmt.Errorf("prepare symbol_meta batch: %w", err)
}

for _, s := range symbols {
if err := batch.Append(
s.SymbolID,
s.Symbol,
s.BaseAsset,
s.OptionType,
s.StrikePrice,
s.Expiration,
s.UnderlyingIndex,
); err != nil {
return fmt.Errorf("append symbol %s: %w", s.Symbol, err)
}
}

if err := batch.Send(); err != nil {
return fmt.Errorf("send symbol_meta batch: %w", err)
}
return nil
}

const barInsertSQL = `INSERT INTO crypto_options_bar_1m (
timestamp, symbol_id, base_asset,
mark_open, mark_high, mark_low, mark_close,
last_open, last_high, last_low, last_close,
bid_open, bid_close, ask_open, ask_close,
mark_iv_open, mark_iv_close, bid_iv_open, ask_iv_open,
delta, gamma, vega, theta, rho,
underlying_price_open, underlying_price_close,
open_interest, tick_count
)`

// InsertBars batch-inserts 1-minute bars into crypto_options_bar_1m.
func InsertBars(ctx context.Context, conn driver.Conn, bars <-chan Bar1m, batchSize int) (int64, error) {
var totalRows int64

batch, err := conn.PrepareBatch(ctx, barInsertSQL)
if err != nil {
return 0, fmt.Errorf("prepare bar_1m batch: %w", err)
}

batchCount := 0
for bar := range bars {
if err := batch.Append(
bar.Timestamp,
bar.SymbolID,
bar.BaseAsset,
bar.MarkOpen, bar.MarkHigh, bar.MarkLow, bar.MarkClose,
bar.LastOpen, bar.LastHigh, bar.LastLow, bar.LastClose,
bar.BidOpen, bar.BidClose, bar.AskOpen, bar.AskClose,
bar.MarkIVOpen, bar.MarkIVClose, bar.BidIVOpen, bar.AskIVOpen,
bar.Delta, bar.Gamma, bar.Vega, bar.Theta, bar.Rho,
bar.UnderlyingPriceOpen, bar.UnderlyingPriceClose,
bar.OpenInterest, bar.TickCount,
); err != nil {
return totalRows, fmt.Errorf("append bar row: %w", err)
}

batchCount++
totalRows++

if batchCount >= batchSize {
if err := batch.Send(); err != nil {
return totalRows, fmt.Errorf("send bar_1m batch: %w", err)
}
log.Printf("[clickhouse] inserted %d rows (total: %d)", batchCount, totalRows)
batchCount = 0

batch, err = conn.PrepareBatch(ctx, barInsertSQL)
if err != nil {
return totalRows, fmt.Errorf("prepare next bar_1m batch: %w", err)
}
}
}

if batchCount > 0 {
if err := batch.Send(); err != nil {
return totalRows, fmt.Errorf("send final bar_1m batch: %w", err)
}
log.Printf("[clickhouse] inserted final %d rows (total: %d)", batchCount, totalRows)
}

return totalRows, nil
}
