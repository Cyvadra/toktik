package thetadata

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// SelectActiveRoots scores roots by recent activity and returns the filtered list.
func SelectActiveRoots(ctx context.Context, cfg SyncConfig) ([]string, []RootActivity, error) {
	if len(cfg.Roots) == 0 {
		return nil, nil, nil
	}

	workers := cfg.Workers
	if workers < 1 {
		workers = 1
	}
	if workers > len(cfg.Roots) {
		workers = len(cfg.Roots)
	}

	rootCh := make(chan string, len(cfg.Roots))
	for _, root := range cfg.Roots {
		rootCh <- root
	}
	close(rootCh)

	var (
		wg         sync.WaitGroup
		mu         sync.Mutex
		activities []RootActivity
		errCh      = make(chan error, workers)
		processed  atomic.Int64
		kept       atomic.Int64
		failed     atomic.Int64
	)

	startedAt := time.Now()
	log.Printf("[root-filter] scoring %d roots with %d workers", len(cfg.Roots), workers)
	doneCh := make(chan struct{})
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				processedCount := int(processed.Load())
				keptCount := int(kept.Load())
				failedCount := int(failed.Load())
				percent := float64(processedCount) * 100 / float64(len(cfg.Roots))
				elapsed := time.Since(startedAt).Round(time.Second)
				eta := "n/a"
				if processedCount > 0 && processedCount < len(cfg.Roots) {
					remaining := len(cfg.Roots) - processedCount
					etaDuration := time.Duration(float64(time.Since(startedAt)) * float64(remaining) / float64(processedCount))
					eta = etaDuration.Round(time.Second).String()
				}
				log.Printf("[root-filter] progress: %d/%d roots (%.1f%%, kept=%d, failed=%d, elapsed=%s, eta=%s)",
					processedCount, len(cfg.Roots), percent, keptCount, failedCount, elapsed, eta)
			case <-doneCh:
				return
			}
		}
	}()

	for workerID := 0; workerID < workers; workerID++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			mcp := NewMCPClient(cfg.MCPURL)
			if err := mcp.Connect(ctx); err != nil {
				errCh <- fmt.Errorf("root-filter worker %d connect MCP: %w", id, err)
				return
			}
			defer mcp.Close()

			client := NewClient(mcp, cfg.RateLimit)
			defer client.Close()
			for root := range rootCh {
				activity, ok, err := scoreRoot(ctx, client, root, cfg.EndDate, cfg.RootMinExpirations, cfg.RootRecentLookbackDays, cfg.RootSampleExpirations)
				if err != nil {
					log.Printf("[root-filter] %s: %v", root, err)
					failed.Add(1)
					processed.Add(1)
					continue
				}
				if !ok {
					processed.Add(1)
					continue
				}
				mu.Lock()
				activities = append(activities, activity)
				mu.Unlock()
				kept.Add(1)
				processed.Add(1)
			}
		}(workerID)
	}

	wg.Wait()
	close(doneCh)

	processedCount := int(processed.Load())
	keptCount := int(kept.Load())
	failedCount := int(failed.Load())
	elapsed := time.Since(startedAt).Round(time.Second)
	log.Printf("[root-filter] complete: %d/%d roots (kept=%d, failed=%d, elapsed=%s)",
		processedCount, len(cfg.Roots), keptCount, failedCount, elapsed)

	close(errCh)
	for err := range errCh {
		if err != nil {
			return nil, nil, err
		}
	}

	sort.Slice(activities, func(i, j int) bool {
		if activities[i].Score == activities[j].Score {
			return activities[i].Root < activities[j].Root
		}
		return activities[i].Score > activities[j].Score
	})

	if cfg.RootTopN > 0 && len(activities) > cfg.RootTopN {
		activities = activities[:cfg.RootTopN]
	}

	roots := make([]string, 0, len(activities))
	for _, activity := range activities {
		roots = append(roots, activity.Root)
	}
	return roots, activities, nil
}

func scoreRoot(ctx context.Context, client *Client, root string, asOf time.Time, minExpirations int, lookbackDays int, sampleExpirations int) (RootActivity, bool, error) {
	expirations, err := client.ListExpirations(ctx, root)
	if err != nil {
		return RootActivity{}, false, fmt.Errorf("list expirations: %w", err)
	}

	activity := RootActivity{
		Root:             root,
		TotalExpirations: len(expirations),
	}
	if len(expirations) < minExpirations {
		return activity, false, nil
	}

	lookbackStart := asOf.AddDate(0, 0, -lookbackDays)
	recent := make([]time.Time, 0, len(expirations))
	for _, exp := range expirations {
		if !exp.Before(lookbackStart) {
			recent = append(recent, exp)
		}
	}
	activity.RecentExpirations = len(recent)
	if activity.RecentExpirations == 0 {
		return activity, false, nil
	}

	sort.Slice(recent, func(i, j int) bool {
		return recent[i].Before(recent[j])
	})

	if sampleExpirations < 1 {
		sampleExpirations = 1
	}
	if len(recent) > sampleExpirations {
		recent = recent[:sampleExpirations]
	}

	for _, exp := range recent {
		strikes, err := client.ListStrikes(ctx, root, exp)
		if err != nil {
			continue
		}
		activity.SampledStrikes += len(strikes)
	}

	activity.Score = activity.RecentExpirations*100000 + activity.SampledStrikes
	return activity, true, nil
}
