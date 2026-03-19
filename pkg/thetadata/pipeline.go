package thetadata

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// Pipeline orchestrates the sync of equity options data from Theta Data into ClickHouse.
type Pipeline struct {
	cfg      SyncConfig
	client   *Client
	store    *Store
	progress *Progress
	limiter  *rate.Limiter
}

// NewPipeline creates a new sync pipeline.
func NewPipeline(cfg SyncConfig, client *Client, store *Store, progress *Progress) *Pipeline {
	return &Pipeline{
		cfg:      cfg,
		client:   client,
		store:    store,
		progress: progress,
		limiter:  rate.NewLimiter(rate.Limit(cfg.RateLimit), 1),
	}
}

// Run executes the pipeline: for each (root, date), fetch EOD + Greeks + OI and store.
func (p *Pipeline) Run(ctx context.Context) error {
	tasks := p.buildTasks()
	if len(tasks) == 0 {
		log.Printf("No tasks to process")
		return nil
	}

	log.Printf("Pipeline: %d (root,date) tasks across %d roots", len(tasks), len(p.cfg.Roots))

	taskCh := make(chan DateTask, len(tasks))
	for _, t := range tasks {
		taskCh <- t
	}
	close(taskCh)

	workers := p.cfg.Workers
	if workers > len(tasks) {
		workers = len(tasks)
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	var totalBars int
	var errCount int

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for task := range taskCh {
				if ctx.Err() != nil {
					return
				}
				bars, err := p.processDate(ctx, task)
				mu.Lock()
				if err != nil {
					errCount++
					log.Printf("[worker-%d] FAIL %s %s: %v", workerID, task.Root, task.Date.Format("2006-01-02"), err)
				} else {
					totalBars += bars
					if p.cfg.Debug {
						log.Printf("[worker-%d] OK   %s %s  bars=%d", workerID, task.Root, task.Date.Format("2006-01-02"), bars)
					}
				}
				mu.Unlock()
			}
		}(i)
	}

	wg.Wait()
	log.Printf("Pipeline complete: %d bars inserted, %d failures, %d total completed",
		totalBars, errCount, p.progress.CompletedCount())

	if errCount > 0 {
		return fmt.Errorf("%d tasks failed", errCount)
	}
	return nil
}

// buildTasks generates the list of (root, date) work items, skipping already-completed ones.
func (p *Pipeline) buildTasks() []DateTask {
	var tasks []DateTask
	for _, root := range p.cfg.Roots {
		for d := p.cfg.StartDate; !d.After(p.cfg.EndDate); d = d.AddDate(0, 0, 1) {
			// Skip weekends.
			if d.Weekday() == time.Saturday || d.Weekday() == time.Sunday {
				continue
			}
			dateStr := d.Format("2006-01-02")
			if p.progress.IsCompleted(root, dateStr) {
				continue
			}
			tasks = append(tasks, DateTask{Root: root, Date: d})
		}
	}
	return tasks
}

// processDate handles a single (root, date): fetch data, store, update progress.
func (p *Pipeline) processDate(ctx context.Context, task DateTask) (int, error) {
	dateStr := task.Date.Format("2006-01-02")

	if err := p.progress.MarkStarted(task.Root, dateStr); err != nil {
		return 0, fmt.Errorf("mark started: %w", err)
	}

	// If there's leftover data from a previous interrupted run, remove it.
	if has, _ := p.store.HasDateData(ctx, task.Root, task.Date); has {
		if err := p.store.DeleteDateData(ctx, task.Root, task.Date); err != nil {
			return 0, fmt.Errorf("delete stale data: %w", err)
		}
	}

	// Throttle API calls.
	if err := p.limiter.Wait(ctx); err != nil {
		return 0, err
	}

	// 1. Fetch EOD data (all contracts for this root on this date, single API call).
	eodRows, err := p.client.GetEOD(ctx, task.Root, dateStr)
	if err != nil {
		_ = p.progress.MarkFailed(task.Root, dateStr, err.Error())
		return 0, fmt.Errorf("get EOD: %w", err)
	}
	if len(eodRows) == 0 {
		// No data for this date (holiday, no trading). Mark completed and move on.
		_ = p.progress.MarkCompleted(task.Root, dateStr, 0)
		return 0, nil
	}

	if err := p.limiter.Wait(ctx); err != nil {
		return 0, err
	}

	// 2. Fetch EOD Greeks (server-calculated with SOFR rates).
	greeksRows, err := p.client.GetGreeksEOD(ctx, task.Root, dateStr)
	if err != nil {
		// Greeks failure is non-fatal — we still have EOD price data.
		if p.cfg.Debug {
			log.Printf("  Greeks EOD unavailable for %s %s: %v", task.Root, dateStr, err)
		}
		greeksRows = nil
	}

	greeksMap := make(map[string]*GreeksEODRow, len(greeksRows))
	for i := range greeksRows {
		g := &greeksRows[i]
		key := contractKeyStr(g.Symbol, g.Expiration, g.Strike, g.Right)
		greeksMap[key] = g
	}

	if err := p.limiter.Wait(ctx); err != nil {
		return 0, err
	}

	// 3. Fetch open interest.
	oiRows, err := p.client.GetOpenInterest(ctx, task.Root, dateStr)
	if err != nil {
		if p.cfg.Debug {
			log.Printf("  OI unavailable for %s %s: %v", task.Root, dateStr, err)
		}
		oiRows = nil
	}

	oiMap := make(map[string]int, len(oiRows))
	for _, r := range oiRows {
		key := contractKeyStr(r.Symbol, r.Expiration, r.Strike, r.Right)
		oiMap[key] = r.OI
	}

	// 4. Insert into ClickHouse.
	if err := p.store.InsertEODBars(ctx, task.Root, task.Date, eodRows, greeksMap, oiMap); err != nil {
		_ = p.progress.MarkFailed(task.Root, dateStr, err.Error())
		return 0, fmt.Errorf("insert: %w", err)
	}

	_ = p.progress.MarkCompleted(task.Root, dateStr, len(eodRows))
	return len(eodRows), nil
}
