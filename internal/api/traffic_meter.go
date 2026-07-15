package api

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"time"
)

const trafficPeakWindow = 5 * time.Second

// TrafficRecord is the final metadata for one HTTP request.
type TrafficRecord struct {
	Method      string
	Route       string
	StatusClass uint16
}

// TrafficMinute is one in-memory aggregate ready for persistence.
type TrafficMinute struct {
	Minute           time.Time
	Method           string
	Route            string
	StatusClass      uint16
	RequestCount     uint64
	IngressBytes     uint64
	EgressBytes      uint64
	PeakIngressBytes uint64
	PeakEgressBytes  uint64
	PeakTotalBytes   uint64
}

type trafficKey struct {
	minute      time.Time
	method      string
	route       string
	statusClass uint16
}

type trafficBucket struct {
	requestCount uint64
	ingressBytes uint64
	egressBytes  uint64
	peaks        map[time.Time]trafficWindow
}

type trafficWindow struct {
	ingressBytes uint64
	egressBytes  uint64
}

// TrafficMeter stores per-minute application payload traffic. It is safe for
// concurrent HTTP handlers. Persistence is intentionally separate so request
// handling never performs a database write.
type TrafficMeter struct {
	mu      sync.Mutex
	buckets map[trafficKey]*trafficBucket
	now     func() time.Time
}

// TrafficMinuteWriter persists completed minute aggregates.
type TrafficMinuteWriter interface {
	WriteTrafficMinutes(ctx context.Context, rows []TrafficMinute) error
}

// NewTrafficMeter creates a traffic meter using wall-clock UTC timestamps.
func NewTrafficMeter() *TrafficMeter {
	return newTrafficMeter(time.Now)
}

// StartTrafficMeterFlusher writes completed minutes on the configured cadence
// and performs a final best-effort flush once stop is closed.
func StartTrafficMeterFlusher(meter *TrafficMeter, writer TrafficMinuteWriter, interval time.Duration, stop <-chan struct{}) {
	if meter == nil || writer == nil {
		return
	}
	if interval <= 0 {
		interval = 15 * time.Second
	}
	flush := func(cutoff time.Time) {
		rows := meter.Snapshot(cutoff)
		if len(rows) == 0 {
			return
		}
		if err := writer.WriteTrafficMinutes(context.Background(), rows); err != nil {
			slog.Error("flush API traffic statistics", "rows", len(rows), "error", err)
			return
		}
		meter.DiscardBefore(cutoff)
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				flush(time.Now().UTC().Add(time.Minute))
				return
			case now := <-ticker.C:
				flush(now.UTC())
			}
		}
	}()
}

func newTrafficMeter(now func() time.Time) *TrafficMeter {
	return &TrafficMeter{
		buckets: make(map[trafficKey]*trafficBucket),
		now:     now,
	}
}

// NewRequest begins measuring one request. Call Finish exactly once.
func (m *TrafficMeter) NewRequest() *TrafficRequest {
	return &TrafficRequest{meter: m}
}

// Snapshot returns completed aggregates before cutoff without clearing them.
// Call DiscardBefore after the rows are persisted successfully. The cutoff
// should normally be the current UTC minute so active-minute data remains in
// memory until it is complete.
func (m *TrafficMeter) Snapshot(cutoff time.Time) []TrafficMinute {
	cutoff = truncateMinute(cutoff)
	m.mu.Lock()
	defer m.mu.Unlock()

	results := make([]TrafficMinute, 0)
	for key, bucket := range m.buckets {
		if !key.minute.Before(cutoff) {
			continue
		}
		result := TrafficMinute{
			Minute:       key.minute,
			Method:       key.method,
			Route:        key.route,
			StatusClass:  key.statusClass,
			RequestCount: bucket.requestCount,
			IngressBytes: bucket.ingressBytes,
			EgressBytes:  bucket.egressBytes,
		}
		for _, peak := range bucket.peaks {
			if peak.ingressBytes > result.PeakIngressBytes {
				result.PeakIngressBytes = peak.ingressBytes
			}
			if peak.egressBytes > result.PeakEgressBytes {
				result.PeakEgressBytes = peak.egressBytes
			}
			if total := peak.ingressBytes + peak.egressBytes; total > result.PeakTotalBytes {
				result.PeakTotalBytes = total
			}
		}
		results = append(results, result)
	}
	return results
}

// DiscardBefore removes completed aggregates after successful persistence.
func (m *TrafficMeter) DiscardBefore(cutoff time.Time) {
	cutoff = truncateMinute(cutoff)
	m.mu.Lock()
	defer m.mu.Unlock()
	for key := range m.buckets {
		if key.minute.Before(cutoff) {
			delete(m.buckets, key)
		}
	}
}

// TrafficRequest accumulates traffic for one request. Byte events retain their
// write/read timestamp so streaming responses remain assigned to the correct
// minute even when the request completes later.
type TrafficRequest struct {
	meter    *TrafficMeter
	mu       sync.Mutex
	finished bool
	events   []trafficEvent
}

type trafficEvent struct {
	at      time.Time
	ingress uint64
	egress  uint64
}

func (r *TrafficRequest) addIngress(at time.Time, bytes uint64) {
	if bytes == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.finished {
		r.events = append(r.events, trafficEvent{at: at, ingress: bytes})
	}
}

func (r *TrafficRequest) addEgress(at time.Time, bytes uint64) {
	if bytes == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.finished {
		r.events = append(r.events, trafficEvent{at: at, egress: bytes})
	}
}

// Finish applies the request metadata and all recorded payload bytes.
func (r *TrafficRequest) Finish(record TrafficRecord) {
	r.mu.Lock()
	if r.finished {
		r.mu.Unlock()
		return
	}
	r.finished = true
	events := r.events
	r.events = nil
	r.mu.Unlock()

	if record.Method == "" {
		record.Method = "UNKNOWN"
	}
	if record.Route == "" {
		record.Route = "unmatched"
	}
	if record.StatusClass == 0 {
		record.StatusClass = 500
	}

	r.meter.mu.Lock()
	defer r.meter.mu.Unlock()
	requestMinute := truncateMinute(r.meter.now())
	requestKey := trafficKey{minute: requestMinute, method: record.Method, route: record.Route, statusClass: record.StatusClass}
	bucket := r.meter.bucket(requestKey)
	bucket.requestCount++
	for _, event := range events {
		key := trafficKey{minute: truncateMinute(event.at), method: record.Method, route: record.Route, statusClass: record.StatusClass}
		bucket = r.meter.bucket(key)
		bucket.ingressBytes += event.ingress
		bucket.egressBytes += event.egress
		window := truncateWindow(event.at)
		peak := bucket.peaks[window]
		peak.ingressBytes += event.ingress
		peak.egressBytes += event.egress
		bucket.peaks[window] = peak
	}
}

func (m *TrafficMeter) bucket(key trafficKey) *trafficBucket {
	bucket := m.buckets[key]
	if bucket == nil {
		bucket = &trafficBucket{peaks: make(map[time.Time]trafficWindow)}
		m.buckets[key] = bucket
	}
	return bucket
}

func truncateMinute(at time.Time) time.Time {
	return at.UTC().Truncate(time.Minute)
}

func truncateWindow(at time.Time) time.Time {
	return at.UTC().Truncate(trafficPeakWindow)
}

type trafficReadCloser struct {
	io.ReadCloser
	request *TrafficRequest
	now     func() time.Time
}

func (r *trafficReadCloser) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.request.addIngress(r.now(), uint64(n))
	}
	return n, err
}
