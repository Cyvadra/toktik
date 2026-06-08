package fmp

import (
	"encoding/json"
	"fmt"
	"math"
)

type flexibleInt64 int64

func (v *flexibleInt64) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*v = 0
		return nil
	}
	var number float64
	if err := json.Unmarshal(data, &number); err != nil {
		return err
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || number > math.MaxInt64 || number < math.MinInt64 {
		return fmt.Errorf("invalid int64 number %s", string(data))
	}
	*v = flexibleInt64(math.Round(number))
	return nil
}
