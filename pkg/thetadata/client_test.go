package thetadata

import "testing"

func TestDecodeResponseFlattensContractEnvelope(t *testing.T) {
	raw := []byte(`{
		"response": [
			{
				"contract": {
					"symbol": "AAPL",
					"expiration": "2025-01-17",
					"strike": 220.0,
					"right": "CALL"
				},
				"data": [
					{
						"timestamp": "2025-01-02T09:35:00.000",
						"bid": 1.2,
						"ask": 1.4,
						"bid_size": 10,
						"ask_size": 15
					}
				]
			}
		]
	}`)

	rows, err := decodeResponse[QuoteRow](raw)
	if err != nil {
		t.Fatalf("decodeResponse returned error: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Symbol != "AAPL" {
		t.Fatalf("unexpected symbol: %q", rows[0].Symbol)
	}
	if rows[0].Expiration != "2025-01-17" {
		t.Fatalf("unexpected expiration: %q", rows[0].Expiration)
	}
	if rows[0].Right != "call" {
		t.Fatalf("expected normalized right=call, got %q", rows[0].Right)
	}
	if rows[0].Timestamp != "2025-01-02T09:35:00.000" {
		t.Fatalf("unexpected timestamp: %q", rows[0].Timestamp)
	}
}
