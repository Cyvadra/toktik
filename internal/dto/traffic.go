package dto

import "time"

// TrafficStatsRequest selects a UTC period of API traffic statistics.
type TrafficStatsRequest struct {
	From string `form:"from" binding:"required"`
	To   string `form:"to" binding:"required"`
}

type TrafficStatsPoint struct {
	Timestamp        time.Time `json:"timestamp"`
	RequestCount     uint64    `json:"request_count"`
	IngressBytes     uint64    `json:"ingress_bytes"`
	EgressBytes      uint64    `json:"egress_bytes"`
	PeakIngressBytes uint64    `json:"peak_ingress_bytes"`
	PeakEgressBytes  uint64    `json:"peak_egress_bytes"`
	PeakTotalBytes   uint64    `json:"peak_total_bytes"`
}

type TrafficStatsSummary struct {
	RequestCount              uint64  `json:"request_count"`
	IngressBytes              uint64  `json:"ingress_bytes"`
	EgressBytes               uint64  `json:"egress_bytes"`
	PeakMinuteIngressBytes    uint64  `json:"peak_minute_ingress_bytes"`
	PeakMinuteEgressBytes     uint64  `json:"peak_minute_egress_bytes"`
	PeakMinuteTotalBytes      uint64  `json:"peak_minute_total_bytes"`
	PeakFiveSecondIngress     uint64  `json:"peak_five_second_ingress_bytes"`
	PeakFiveSecondEgress      uint64  `json:"peak_five_second_egress_bytes"`
	PeakFiveSecondTotal       uint64  `json:"peak_five_second_total_bytes"`
	PeakFiveSecondIngressMbps float64 `json:"peak_five_second_ingress_mbps"`
	PeakFiveSecondEgressMbps  float64 `json:"peak_five_second_egress_mbps"`
	PeakFiveSecondTotalMbps   float64 `json:"peak_five_second_total_mbps"`
}

type TrafficStatsResponse struct {
	From     time.Time           `json:"from"`
	To       time.Time           `json:"to"`
	Interval string              `json:"interval"`
	Data     []TrafficStatsPoint `json:"data"`
	Summary  TrafficStatsSummary `json:"summary"`
}
