# BTC Moving Average Deviation Spread Strategy

---

# 1. BTC Moving Average Deviation Bullish Spread Strategy (Bull Put Spread)

## 1. Strategy Core Logic

Strategy Type: Trend-following momentum strategy.

Entry Signal:  
When **P > 0.15**, the market is considered to be in a strong short-term uptrend.  
Execute a **Bull Put Spread** (sell put spread).

### Core Indicator Calculation

Moving Average (maLine):  
Simple Moving Average (SMA), period **n = 120**.

Volatility Baseline:

M = max(
Highest(H,120) - Lowest(C,120),
Highest(C,120) - Lowest(L,120)
)

i.e.

M = max(
(Highest High of last 120 bars − Lowest Close of last 120 bars),
(Highest Close of last 120 bars − Lowest Low of last 120 bars)
)

Deviation Ratio:

P = (Close - ma) / M

---

## 2. Full Trading Specification

| Dimension | Execution Parameters |
|-----------|---------------------|
| Execution timeframe | 1-hour candles (1h) |
| Entry condition | P > 0.15 |
| Short Leg (sell put) | Put option selected from options with expiry **closest to 15 days**. Delta between **-0.5 and -0.4**. Premium **N (option price, bid)** must be **≥ 0.025** |
| Long Leg (protective put) | Out-of-the-money Put with **same expiry** as the short leg. Delta between **-0.15 and -0.1** |
| Position closing rule | When the **sell put leg** reaches **> 88% unrealized profit**, close the sell leg first (buy to close). After the sell leg is closed, the long leg will be closed **24 hours later**, or **immediately if its profit reaches 50%** |
| Maximum holding time | Maximum holding period **48 hours**. Otherwise close all positions automatically |
| Position sizing | Each trade opens **1 spread (1 short + 1 long contract)** |

---

# 2. BTC Moving Average Deviation Bearish Spread Strategy (Bear Call Spread)

## 1. Strategy Core Logic

Strategy Type:  
Counter-trend rebound strategy or bearish trend-following strategy.

Entry Signal:  
When **P < -0.15**, the market is considered to be in a severe short-term decline or strong downtrend.  
Execute a **Bear Call Spread**.

Indicator calculation is **identical to the bullish strategy**, only the signal direction is reversed.

---

## 2. Full Trading Specification

| Dimension | Execution Parameters |
|-----------|---------------------|
| Execution timeframe | 1-hour candles (1h) |
| Entry condition | P < -0.15 |
| Short Leg (sell call) | Call option selected from options with expiry **closest to 15 days**. Delta between **0.4 and 0.5**. Premium **N (bid price)** must be **≥ 0.025** |
| Long Leg (protective call) | Out-of-the-money Call with **same expiry** as the short leg. Delta between **0.1 and 0.15** |
| Position closing rule | When the **sell call leg** reaches **> 88% unrealized profit**, close the sell leg first (buy to close). After closing the sell leg, the long leg will be closed **24 hours later**, or **immediately if its profit reaches 50%** |
| Maximum holding time | Maximum holding period **48 hours**, after which all positions are closed automatically |
| Position sizing | Each trade opens **1 spread (1 short + 1 long contract)** |

---

# 3. Notes and Implementation Details

### Dynamic Option Selection

The strategy dynamically selects which options to trade.

Example process when monitoring BTC:

1. Retrieve all options with **base_asset = BTC**
2. Filter by **expiration date**
   - Start searching from **current time + 15 days**
   - If no match is found, continue searching earlier expirations
3. Apply **delta and price filters**

If **no options satisfy the filtering conditions**, skip the trade opportunity.

However, the selected expiration must still be **at least 7 days away**.

---

### Multiple Candidate Options

If multiple options satisfy the selection criteria:

1. Rank by **order book spread quality**

Spread metric:

(ask - bid) / (ask + bid)

Select the option with the **smallest spread ratio**.

2. If the spread ratios are equal, choose the option with **higher trading volume**.

---

### Indicator Source

All indicator calculations are based on the **underlying asset price**, not the option price.

Specifically:

- Moving average
- Volatility baseline M
- Deviation ratio P

All use **underlying_price** data.

---

### PnL Calculation

All unrealized profit/loss calculations are based on:

mark_close

(i.e. mark price used as the closing reference).