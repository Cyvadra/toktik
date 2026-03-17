package thetadata

import (
	"context"
	"fmt"
	"log"
	"sort"
	"sync"
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
	)

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
					continue
				}
				if !ok {
					continue
				}
				mu.Lock()
				activities = append(activities, activity)
				mu.Unlock()
			}
		}(workerID)
	}

	wg.Wait()
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
