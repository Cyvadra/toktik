## **BTC Coin-Margined Enhancement Strategy**

**Core Parameters**
* **Initial Capital:** 100 BTC
* **Position Size:** 10 BTC (notional value) per entry.
* **Divergence Definition:** 1.  **MACD Divergence:** Price makes a Higher High (HH), but MACD Diff makes a Lower High (or vice versa).
    2.  **Volatility Filter:** Current Standard Deviation ($std / mastd$) must be above the **50th percentile** (last 100 bars).

---

### **Scenario 1: High RSI (Bullish/Neutral Trend)**
* **Condition:** $RSI(200) > 50$
* **Signal Level:** 12h or 24h charts.
* **Entry Trigger:** Bearish Divergence (MACD or CCI) followed by one **Bearish Candle**.
* **Trade Execution:**
    * **Sell Call:** Delta 0.3 (~40 days to expiry).
    * **Buy Put:** Delta -0.25 (~40 days to expiry).
    * **Budgeting:** Use **70%** of the premium collected from the Sell Call to fund the Put.
* **Exit (Stop):** Spot price bounces **$3 \times ATR$** from the post-entry low.

---

### **Scenario 2: Low RSI (Bearish/Weak Trend)**
* **Condition:** $RSI(200) < 50$
* **Signal Level:** 3h or 6h charts.
* **Entry Trigger:** Bearish Divergence signal.
* **Trade Execution:**
    * **Sell Call:** Delta 0.3 (~25 days to expiry).
    * **Buy Put:** Delta -0.25 (~25 days to expiry).
    * **Budgeting:** Use **70%** of the premium collected from the Sell Call to fund the Put.
* **Exit (Stop):** Spot price drops **$3 \times ATR$** from the post-entry high.

---

### **Dynamic Management**

#### **1. Long Put Management**
* **Auto-Roll/Rebalance:** If Put Delta exceeds **-0.5** (absolute value > 0.5) OR floating profit exceeds **50%**.
* **Action:** Close and restart the entry process.

#### **2. Short Call Management (Profit Taking)**
* **Partial Close:** Close **50%** of the position if floating profit > **70%**.
* **Full Close:** Close **100%** of the position if floating profit > **88%**.
