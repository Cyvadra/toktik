import unittest

from export_dsl_signal_audit import build_rows


def trace(stage: str, bar_index: int, symbol: str, reason: str) -> dict:
    return {
        "code": f"dsl.trace.{stage}",
        "bar_index": bar_index,
        "message": f"{stage} {symbol}: {reason}",
    }


def payload(*warnings: dict) -> list[dict]:
    return [{"status": {"result": {"summaries": [{"warnings": list(warnings)}]}}}]


class SignalAuditTests(unittest.TestCase):
    def test_build_rows_merges_events_in_bar_order(self) -> None:
        rows = build_rows(
            payload(
                trace("signal_input", 8, "MSFT", "turnover=2.00;valuation=50.00;ivp=25.00;hvp=30.00;rsi=55.00;cci=20.00"),
                trace("signal_match", 8, "MSFT", "turnover=2.00;valuation=50.00;ivp=25.00;hvp=30.00;rsi=55.00;cci=20.00;strategies=BUY_STRADDLE|SHORT_STRANGLE"),
                trace("signal_open", 8, "MSFT", "strategy=BUY_STRADDLE"),
                trace("signal_reject", 8, "MSFT", "reason=dte_premium"),
                trace("signal_reject", 7, "AAPL", "reason=chain_empty"),
            )
        )
        self.assertEqual([row["symbol"] for row in rows], ["AAPL", "MSFT"])
        self.assertEqual(rows[1]["matched_strategies"], "BUY_STRADDLE|SHORT_STRANGLE")
        self.assertEqual(rows[1]["final_action"], "BUY_STRADDLE")
        self.assertEqual(rows[1]["rejection_reason"], "")
        self.assertEqual(rows[0]["rejection_reason"], "chain_empty")

    def test_rejects_open_without_matching_strategy(self) -> None:
        with self.assertRaisesRegex(ValueError, "outside matched strategies"):
            build_rows(
                payload(
                    trace("signal_input", 8, "MSFT", "turnover=2.00;valuation=50.00;ivp=25.00;hvp=30.00;rsi=55.00;cci=20.00"),
                    trace("signal_match", 8, "MSFT", "strategies=BUY_PUT"),
                    trace("signal_open", 8, "MSFT", "strategy=BUY_STRADDLE"),
                )
            )

    def test_rejects_multiple_opens_in_one_bar(self) -> None:
        with self.assertRaisesRegex(ValueError, "has 2 signal_open events"):
            build_rows(
                payload(
                    trace("signal_open", 8, "MSFT", "strategy=BUY_PUT"),
                    trace("signal_open", 8, "AAPL", "strategy=BUY_PUT"),
                )
            )

    def test_rejects_truncated_trace(self) -> None:
        with self.assertRaisesRegex(ValueError, "trace diagnostics were truncated"):
            build_rows(payload({"code": "dsl.trace.truncated", "bar_index": 8, "message": "trace diagnostics exceeded the configured limit"}))


if __name__ == "__main__":
    unittest.main()