package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/Cyvadra/toktik/internal/backtest"
	appCli "github.com/Cyvadra/toktik/internal/cli"
	"github.com/Cyvadra/toktik/internal/datafeed"
)

// GoldenCrossStrategy is a simple SMA crossover strategy.
// Buys when fast SMA crosses above slow SMA, sells when it crosses below.
type GoldenCrossStrategy struct {
	fastPeriod int
	slowPeriod int
	entryTWAP  int
}

func (s *GoldenCrossStrategy) Name() string { return "GoldenCross" }

func (s *GoldenCrossStrategy) Init(ctx *backtest.SetupContext) error {
	ctx.SetParam("fast_period", s.fastPeriod)
	ctx.SetParam("slow_period", s.slowPeriod)

	ctx.Register("sma_fast", backtest.SMA("close", s.fastPeriod))
	ctx.Register("sma_slow", backtest.SMA("close", s.slowPeriod))
	ctx.Register("buy_signal", backtest.Crossover("sma_fast", "sma_slow"))
	ctx.Register("sell_signal", backtest.Crossunder("sma_fast", "sma_slow"))
	return nil
}

func (s *GoldenCrossStrategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()

	if ctx.Ind("buy_signal") == 1 && ctx.Position(primary) == 0 {
		price := ctx.Close()
		if price > 0 {
			qty := (ctx.Equity() * 0.95) / price
			if s.entryTWAP > 1 {
				ctx.BuyTWAP(primary, qty, s.entryTWAP)
			} else {
				ctx.Buy(primary, qty)
			}
		}
	}

	if ctx.Ind("sell_signal") == 1 && ctx.Position(primary) > 0 {
		ctx.ClosePosition(primary)
	}
}

// DeltaFilterStrategy demonstrates options-specific field access.
// Only trades when option delta is in a favorable range.
type DeltaFilterStrategy struct {
	entryTWAP int
}

func (s *DeltaFilterStrategy) Name() string { return "DeltaFilter" }

func (s *DeltaFilterStrategy) Init(ctx *backtest.SetupContext) error {
	ctx.Register("ema20", backtest.EMA("close", 20))
	ctx.Register("rsi14", backtest.RSI("close", 14))
	ctx.Register("delta_ok", backtest.Custom(
		[]string{"delta"},
		func(inputs map[string][]float64) []float64 {
			d := inputs["delta"]
			out := make([]float64, len(d))
			for i, v := range d {
				if v > 0.3 && v < 0.7 {
					out[i] = 1
				}
			}
			return out
		},
	))
	return nil
}

func (s *DeltaFilterStrategy) OnBar(ctx *backtest.BarContext) {
	primary := ctx.PrimaryRef()
	deltaOK := ctx.Ind("delta_ok")
	rsi := ctx.Ind("rsi14")

	if deltaOK == 1 && rsi < 30 && ctx.Position(primary) == 0 {
		price := ctx.Close()
		if price > 0 {
			qty := (ctx.Equity() * 0.5) / price
			if s.entryTWAP > 1 {
				ctx.BuyTWAP(primary, qty, s.entryTWAP)
			} else {
				ctx.Buy(primary, qty)
			}
		}
	}

	if (deltaOK == 0 || rsi > 70) && ctx.Position(primary) > 0 {
		ctx.ClosePosition(primary)
	}
}

func main() {
	runtimeCfg := appCli.MustLoadRuntime()
	dsn := flag.String("clickhouse-dsn", runtimeCfg.ClickHouse.DSN, "ClickHouse DSN")
	symbol := flag.String("symbol", "BTC-3JAN25-100000-C", "Option symbol to backtest")
	interval := flag.String("interval", "15m", "Bar interval")
	fromStr := flag.String("from", "2024-12-01", "Start date (YYYY-MM-DD)")
	toStr := flag.String("to", "2025-02-03", "End date (YYYY-MM-DD)")
	capital := flag.Float64("capital", 100000, "Initial capital")
	stratName := flag.String("strategy", "golden-cross", "Strategy: golden-cross or delta-filter")
	fillMode := flag.String("fill-mode", "bidask", "Execution price mode: canonical or bidask")
	valuationMode := flag.String("valuation-mode", "exit", "Valuation mode: close, mid, or exit")
	triggerMode := flag.String("trigger-mode", "canonical", "Trigger mode: canonical or bidask-envelope (uses bid/ask open-close envelope)")
	entryTWAPBars := flag.Int("entry-twap-bars", 1, "Slice entry market orders evenly across N bars")
	outputJSON := flag.String("output", "", "Optional JSON output file path")
	flag.Parse()

	from, err := time.Parse("2006-01-02", *fromStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid --from: %v\n", err)
		os.Exit(1)
	}
	to, err := time.Parse("2006-01-02", *toStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid --to: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()

	conn, err := appCli.ConnectClickHouse(ctx, *dsn, nil)
	if err != nil {
		log.Fatalf("%v", err)
	}

	engine := backtest.NewEngine(backtest.Config{
		InitialCapital:  *capital,
		CommissionModel: backtest.CommissionNone,
		CommissionValue: 0,
		SlippagePct:     0.0005,
		ExecutionMode:   mustParseExecutionMode(*fillMode),
		ValuationMode:   mustParseValuationMode(*valuationMode),
		TriggerMode:     mustParseTriggerMode(*triggerMode),
	})

	feed := datafeed.NewCryptoOptionsDataFeed(conn)
	engine.RegisterDataFeed("crypto-options", feed)

	var strategy backtest.Strategy
	switch *stratName {
	case "golden-cross":
		strategy = &GoldenCrossStrategy{fastPeriod: 10, slowPeriod: 50, entryTWAP: *entryTWAPBars}
	case "delta-filter":
		strategy = &DeltaFilterStrategy{entryTWAP: *entryTWAPBars}
	default:
		fmt.Fprintf(os.Stderr, "unknown strategy: %s\n", *stratName)
		os.Exit(1)
	}

	log.Printf("Running %s on %s (%s) from %s to %s",
		strategy.Name(), *symbol, *interval,
		from.Format("2006-01-02"), to.Format("2006-01-02"))

	result, err := engine.Run(ctx, "crypto-options", *symbol, *interval, from, to, strategy, nil)
	if err != nil {
		log.Fatalf("Backtest failed: %v", err)
	}

	fmt.Println("\n" + result.Summary())

	if *outputJSON != "" {
		if err := result.ExportJSON(*outputJSON); err != nil {
			log.Fatalf("Export JSON failed: %v", err)
		}
		log.Printf("Results exported to %s", *outputJSON)
	}
}

func mustParseExecutionMode(value string) backtest.ExecutionPriceModel {
	switch value {
	case "canonical":
		return backtest.ExecutionPriceCanonical
	case "bidask":
		return backtest.ExecutionPriceBidAsk
	default:
		log.Fatalf("unsupported --fill-mode: %s", value)
		return backtest.ExecutionPriceCanonical
	}
}

func mustParseValuationMode(value string) backtest.ValuationPriceModel {
	switch value {
	case "close":
		return backtest.ValuationPriceClose
	case "mid":
		return backtest.ValuationPriceMid
	case "exit":
		return backtest.ValuationPriceExit
	default:
		log.Fatalf("unsupported --valuation-mode: %s", value)
		return backtest.ValuationPriceClose
	}
}

func mustParseTriggerMode(value string) backtest.TriggerPriceMode {
	switch value {
	case "canonical":
		return backtest.TriggerPriceCanonical
	case "bidask-envelope":
		return backtest.TriggerPriceBidAskEnvelope
	default:
		log.Fatalf("unsupported --trigger-mode: %s", value)
		return backtest.TriggerPriceCanonical
	}
}
