package chquery

import "fmt"

const (
	usStockSplitNumeratorAlias   = "latest_numerator"
	usStockSplitDenominatorAlias = "latest_denominator"
)

// USStockSplitPriceFactor returns the price multiplier for one split event.
// For a conventional 2-for-1 split, numerator=2 and denominator=1, so prices
// before the split are multiplied by 0.5.
func USStockSplitPriceFactor(numerator, denominator float64) (float64, bool) {
	if numerator <= 0 || denominator <= 0 {
		return 0, false
	}
	return denominator / numerator, true
}

func USStockSplitJoinSQL(barAlias, splitAlias string) string {
	return fmt.Sprintf(`LEFT JOIN (
    SELECT
        symbol,
        split_date,
        argMax(numerator, updated_at) AS %s,
        argMax(denominator, updated_at) AS %s
    FROM us_stock_splits
    WHERE numerator > 0 AND denominator > 0
      AND split_date <= (today() + 3)
    GROUP BY symbol, split_date
) AS %s ON %s.symbol = %s.symbol AND %s.split_date > toDate(%s.timestamp)`, usStockSplitNumeratorAlias, usStockSplitDenominatorAlias, splitAlias, splitAlias, barAlias, splitAlias, barAlias)
}

func USStockSplitFactorSQL(splitAlias string) string {
	return fmt.Sprintf("exp(sum(if(%s.%s > 0 AND %s.%s > 0, log(toFloat64(%s.%s) / toFloat64(%s.%s)), 0.0)))", splitAlias, usStockSplitNumeratorAlias, splitAlias, usStockSplitDenominatorAlias, splitAlias, usStockSplitDenominatorAlias, splitAlias, usStockSplitNumeratorAlias)
}

func USStockAdjustedPriceSQL(barAlias, column, splitAlias string) string {
	return fmt.Sprintf("toFloat64(%s.%s) * %s", barAlias, column, USStockSplitFactorSQL(splitAlias))
}
