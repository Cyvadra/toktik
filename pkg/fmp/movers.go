package fmp

import "context"

// Mover is one entry in the biggest-gainers, biggest-losers, or most-active lists.
type Mover struct {
	Symbol            string  `json:"symbol"`
	Name              string  `json:"name"`
	Price             float64 `json:"price"`
	Change            float64 `json:"change"`
	ChangesPercentage float64 `json:"changesPercentage"`
	Exchange          string  `json:"exchange"`
}

// BiggestGainers returns the top percentage-gaining stocks for the current session.
func (c *Client) BiggestGainers(ctx context.Context) ([]Mover, error) {
	var out []Mover
	if err := c.get(ctx, "/biggest-gainers", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// BiggestLosers returns the top percentage-losing stocks for the current session.
func (c *Client) BiggestLosers(ctx context.Context) ([]Mover, error) {
	var out []Mover
	if err := c.get(ctx, "/biggest-losers", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MostActive returns the most actively-traded stocks by volume for the current session.
func (c *Client) MostActive(ctx context.Context) ([]Mover, error) {
	var out []Mover
	if err := c.get(ctx, "/most-actives", nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}
