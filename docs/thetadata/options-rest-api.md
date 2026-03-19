# ThetaData 期权 REST API 调用手册 (v3)

- 文档来源: https://docs.thetadata.us/operations/option_list_symbols.html 与 https://docs.thetadata.us/openapiv3.yaml
- REST Base URL: `http://127.0.0.1:25503/v3`
- 终端要求: 本地 v3 Theta Terminal 需运行
- 接口总数: 34

## 目录总览

| OperationId | 分类 | 路径 | 最低权限 |
|---|---|---|---|
| `option_at_time_quote` | Option / At-Time | `/option/at_time/quote` | `value` |
| `option_at_time_trade` | Option / At-Time | `/option/at_time/trade` | `standard` |
| `option_history_eod` | Option / History | `/option/history/eod` | `free` |
| `option_history_greeks_all` | Option / History | `/option/history/greeks/all` | `professional` |
| `option_history_greeks_eod` | Option / History | `/option/history/greeks/eod` | `standard` |
| `option_history_greeks_first_order` | Option / History | `/option/history/greeks/first_order` | `standard` |
| `option_history_greeks_implied_volatility` | Option / History | `/option/history/greeks/implied_volatility` | `standard` |
| `option_history_greeks_second_order` | Option / History | `/option/history/greeks/second_order` | `professional` |
| `option_history_greeks_third_order` | Option / History | `/option/history/greeks/third_order` | `professional` |
| `option_history_ohlc` | Option / History | `/option/history/ohlc` | `value` |
| `option_history_open_interest` | Option / History | `/option/history/open_interest` | `value` |
| `option_history_quote` | Option / History | `/option/history/quote` | `value` |
| `option_history_trade` | Option / History | `/option/history/trade` | `standard` |
| `option_history_trade_greeks_all` | Option / History | `/option/history/trade_greeks/all` | `professional` |
| `option_history_trade_greeks_first_order` | Option / History | `/option/history/trade_greeks/first_order` | `professional` |
| `option_history_trade_greeks_implied_volatility` | Option / History | `/option/history/trade_greeks/implied_volatility` | `professional` |
| `option_history_trade_greeks_second_order` | Option / History | `/option/history/trade_greeks/second_order` | `professional` |
| `option_history_trade_greeks_third_order` | Option / History | `/option/history/trade_greeks/third_order` | `professional` |
| `option_history_trade_quote` | Option / History | `/option/history/trade_quote` | `standard` |
| `option_list_contracts` | Option / List | `/option/list/contracts/{request_type}` | `value` |
| `option_list_dates` | Option / List | `/option/list/dates/{request_type}` | `free` |
| `option_list_expirations` | Option / List | `/option/list/expirations` | `free` |
| `option_list_strikes` | Option / List | `/option/list/strikes` | `free` |
| `option_list_symbols` | Option / List | `/option/list/symbols` | `free` |
| `option_snapshot_greeks_all` | Option / Snapshot | `/option/snapshot/greeks/all` | `professional` |
| `option_snapshot_greeks_first_order` | Option / Snapshot | `/option/snapshot/greeks/first_order` | `standard` |
| `option_snapshot_greeks_implied_volatility` | Option / Snapshot | `/option/snapshot/greeks/implied_volatility` | `standard` |
| `option_snapshot_greeks_second_order` | Option / Snapshot | `/option/snapshot/greeks/second_order` | `professional` |
| `option_snapshot_greeks_third_order` | Option / Snapshot | `/option/snapshot/greeks/third_order` | `professional` |
| `option_snapshot_market_value` | Option / Snapshot | `/option/snapshot/market_value` | `standard` |
| `option_snapshot_ohlc` | Option / Snapshot | `/option/snapshot/ohlc` | `value` |
| `option_snapshot_open_interest` | Option / Snapshot | `/option/snapshot/open_interest` | `value` |
| `option_snapshot_quote` | Option / Snapshot | `/option/snapshot/quote` | `value` |
| `option_snapshot_trade` | Option / Snapshot | `/option/snapshot/trade` | `standard` |

---

## Quote (option_at_time_quote)

- 文档页: https://docs.thetadata.us/operations/option_at_time_quote.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/at_time/quote`
- 最低权限级别: `value`
- 描述: - Returns the last NBBO quote reported by [OPRA](/Articles/Data-And-Requests/The-SIPs.html) at a specified millisecond of the day.
- The ``time_of_day``parameter represents the 00:00:00.000 ET that the quote should be provided for.
- 示例调用:
  - Returns the last quote for an option contract: `http://127.0.0.1:25503/v3/option/at_time/quote?symbol=AAPL&expiration=20241108&strike=220.000&right=call&start_date=20241104&end_date=20241104&time_of_day=09:30:01.000`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/at_time/quote?symbol=AAPL&expiration=20241108&strike=220.000&right=call&start_date=20241104&end_date=20241104&time_of_day=09:30:01.000&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `start_date` | `query` | `string` | `yes` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `yes` | `` | `` | The end date (inclusive). |
| `time_of_day` | `query` | `string` | `yes` | `` | `` | The time of the day to fetch data for; assumed to be America/New_York. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: Returns the last quote for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `bid_size` | `integer` | The last NBBO bid size. |
| `bid_exchange` | `integer` | The last NBBO bid [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html). |
| `bid` | `number` | The last NBBO bid price. |
| `bid_condition` | `integer` | The last NBBO bid [condition](/Articles/Errors-Exchanges-Conditions/Quote-Conditions.html). |
| `ask_size` | `integer` | The last NBBO ask size. |
| `ask_exchange` | `integer` | The last NBBO ask [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html). |
| `ask` | `number` | The last NBBO ask price. |
| `ask_condition` | `integer` | The last NBBO ask [condition](/Articles/Errors-Exchanges-Conditions/Quote-Conditions.html). |

---

## Trade (option_at_time_trade)

- 文档页: https://docs.thetadata.us/operations/option_at_time_trade.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/at_time/trade`
- 最低权限级别: `standard`
- 描述: - Returns the last trade reported by [OPRA](/Articles/Data-And-Requests/The-SIPs.html) at a specified millisecond of the day.
- Trade condition mappings can be found [here](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html).
- Extended trade conditions are not reported by [OPRA](/Articles/Data-And-Requests/The-SIPs.html) for options, so they can be ignored.
- The ``time_of_day``parameter represents the 00:00:00.000 ET that the trade should be provided for.
- 示例调用:
  - Returns the last trade for an option contract: `http://127.0.0.1:25503/v3/option/at_time/trade?symbol=AAPL&expiration=20241108&strike=220.000&right=call&start_date=20241104&end_date=20241104&time_of_day=09:30:01.000`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/at_time/trade?symbol=AAPL&expiration=20241108&strike=220.000&right=call&start_date=20241104&end_date=20241104&time_of_day=09:30:01.000&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `start_date` | `query` | `string` | `yes` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `yes` | `` | `` | The end date (inclusive). |
| `time_of_day` | `query` | `string` | `yes` | `` | `` | The time of the day to fetch data for; assumed to be America/New_York. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: Returns the last trade for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `sequence` | `integer` | The exchange [sequence](/Articles/Data-And-Requests/Making-Requests.html#trade-sequences). |
| `ext_condition1` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition2` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition3` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition4` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `condition` | `integer` | The trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html). |
| `size` | `integer` | The amount of contracts / shares traded. |
| `exchange` | `integer` | The [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html) the trade was executed. |
| `price` | `number` | The trade price. |

---

## End of Day (option_history_eod)

- 文档页: https://docs.thetadata.us/operations/option_history_eod.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/eod`
- 最低权限级别: `free`
- 描述: - Since [OPRA](/Articles/Data-And-Requests/The-SIPs.html) does not provide a national EOD report for options, Theta Data generates a national EOD report at 17:15 ET each day.
- ``created`` represents the datetime the report was generated and ``last_trade`` represents the datetime of the last trade. 
- The quote in the response represents the last NBBO reported by OPRA at the time of report generation. 
- You can read more about EOD & OHLC data [here](/Articles/Data-And-Requests/OHLC-EOD.html).
- 示例调用:
  - Returns EOD report for an option contract: `http://127.0.0.1:25503/v3/option/history/eod?symbol=AAPL&expiration=20241115&strike=170.000&right=call&start_date=20241104&end_date=20241104`
  - Returns EOD report for all option contracts: `http://127.0.0.1:25503/v3/option/history/eod?symbol=AAPL&expiration=*&start_date=20241104&end_date=20241104`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/history/eod?symbol=AAPL&expiration=*&start_date=20241104&end_date=20241104&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `start_date` | `query` | `string` | `yes` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `yes` | `` | `` | The end date (inclusive). |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: Returns EOD report for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `created` | `string` | The date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `last_trade` | `string` | The last trade date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `open` | `number` | The opening trade price. |
| `high` | `number` | The highest traded price. |
| `low` | `number` | The lowest traded price. |
| `close` | `number` | The closing traded price. |
| `volume` | `integer` | The amount of contracts / shares traded. |
| `count` | `integer` | The amount of trades. |
| `bid_size` | `integer` | The last NBBO bid size. |
| `bid_exchange` | `integer` | The last NBBO bid [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html). |
| `bid` | `number` | The last NBBO bid price. |
| `bid_condition` | `integer` | The last NBBO bid [condition](/Articles/Errors-Exchanges-Conditions/Quote-Conditions.html). |
| `ask_size` | `integer` | The last NBBO ask size. |
| `ask_exchange` | `integer` | The last NBBO ask [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html). |
| `ask` | `number` | The last NBBO ask price. |
| `ask_condition` | `integer` | The last NBBO ask [condition](/Articles/Errors-Exchanges-Conditions/Quote-Conditions.html). |

---

## All Greeks (option_history_greeks_all)

- 文档页: https://docs.thetadata.us/operations/option_history_greeks_all.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/greeks/all`
- 最低权限级别: `professional`
- 描述: - Returns the data for all contracts that share the same provided symbol and expiration. 
- Calculated using the option and underlying midpoint price. If an interval size is specified (*highly recommended*), the option quote used in the calculation follows the same rules as the [quote](/operations/option_history_quote.html) endpoint. 
- The underlying price represents whatever the last underlying price was at the ``timestamp`` field. You can read more about how Theta Data calculates greeks [here](/Articles/Data-And-Requests/Option-Greeks.html).
- Multi-day requests are limited to 1 month of data.
- 示例调用:
  - Returns all greeks for an option contract: `http://127.0.0.1:25503/v3/option/history/greeks/all?symbol=AAPL&expiration=20241108&date=20241104&interval=10m`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/history/greeks/all?symbol=AAPL&expiration=20241108&date=20241104&interval=10m&format=html`
  - Returns all greeks for an option contract between the specified dates (inclusive): `http://127.0.0.1:25503/v3/option/history/greeks/all?symbol=AAPL&expiration=20241108&start_date=20241104&end_date=20241111&interval=10m`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `start_time` | `query` | `string` | `no` | `09:30:00` | `` | The start time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `end_time` | `query` | `string` | `no` | `16:00:00` | `` | The end time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `interval` | `query` | `string` | `yes` | `1s` | `tick, 10ms, 100ms, 500ms, 1s, 5s, 10s, 15s, 30s, 1m, 5m, 10m, 15m, 30m, 1h` | The size of the time interval must be one of the available options listed below. Intervals less than 1m are available only for single-day requests. |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns all greeks for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `bid` | `number` | The last NBBO bid price. |
| `ask` | `number` | The last NBBO ask price. |
| `delta` | `number` | The delta. |
| `theta` | `number` | The Theta. |
| `vega` | `number` | The vega. |
| `rho` | `number` | The rho. |
| `epsilon` | `number` | The epsilon. |
| `lambda` | `number` | The lambda. |
| `gamma` | `number` | The gamma. |
| `vanna` | `number` | The vanna. |
| `charm` | `number` | The charm. |
| `vomma` | `number` | The vomma. |
| `veta` | `number` | The veta. |
| `vera` | `number` | The vera. |
| `speed` | `number` | The speed. |
| `zomma` | `number` | The zomma. |
| `color` | `number` | The color. |
| `ultima` | `number` | The ultima. |
| `d1` | `string` | The d1. |
| `d2` | `string` | The d2. |
| `dual_delta` | `number` | The dual delta. |
| `dual_gamma` | `number` | The dual gamma. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `number` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## End of Day Greeks (option_history_greeks_eod)

- 文档页: https://docs.thetadata.us/operations/option_history_greeks_eod.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/greeks/eod`
- 最低权限级别: `standard`
- 描述: - Returns the data for all contracts that share the same provided symbol and expiration. 
- Uses Theta Data's EOD reports that get generated at 17:15 ET each day. The closing option price and closing underlying price are used for the greeks calculation.
- **Set `expiration` to ``*`` if you want to retrieve data for every option that shares the same ``symbol``. (note: Any ``expiration=*`` must be requested day by day)**
- 示例调用:
  - Returns EOD report for an option contract: `http://127.0.0.1:25503/v3/option/history/greeks/eod?symbol=AAPL&expiration=20241108&strike=220.000&right=call&start_date=20241104&end_date=20241104`
  - Returns EOD report for all option contracts: `http://127.0.0.1:25503/v3/option/history/greeks/eod?symbol=AAPL&expiration=*&start_date=20241104&end_date=20241104`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/history/greeks/eod?symbol=AAPL&expiration=*&start_date=20241104&end_date=20241104&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `start_date` | `query` | `string` | `yes` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `yes` | `` | `` | The end date (inclusive). |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `underlyer_use_nbbo` | `query` | `boolean` | `no` | `False` | `` | Used to select underlyer pricing for Greeks calculation. "true" uses the midpoint of the NBBO; "false" uses the last trade price. |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: Returns EOD report for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `open` | `number` | The opening trade price. |
| `high` | `number` | The highest traded price. |
| `low` | `number` | The lowest traded price. |
| `close` | `number` | The closing traded price. |
| `volume` | `integer` | The amount of contracts / shares traded. |
| `count` | `integer` | The amount of trades. |
| `bid_size` | `integer` | The last NBBO bid size. |
| `bid_exchange` | `integer` | The last NBBO bid [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html). |
| `bid` | `number` | The last NBBO bid price. |
| `bid_condition` | `integer` | The last NBBO bid [condition](/Articles/Errors-Exchanges-Conditions/Quote-Conditions.html). |
| `ask_size` | `integer` | The last NBBO ask size. |
| `ask_exchange` | `integer` | The last NBBO ask [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html). |
| `ask` | `number` | The last NBBO ask price. |
| `ask_condition` | `integer` | The last NBBO ask [condition](/Articles/Errors-Exchanges-Conditions/Quote-Conditions.html). |
| `delta` | `number` | The delta. |
| `theta` | `string` | The Theta. |
| `vega` | `number` | The vega. |
| `rho` | `number` | The rho. |
| `epsilon` | `string` | The epsilon. |
| `lambda` | `number` | The lambda. |
| `gamma` | `number` | The gamma. |
| `vanna` | `string` | The vanna. |
| `charm` | `number` | The charm. |
| `vomma` | `number` | The vomma. |
| `veta` | `number` | The veta. |
| `vera` | `number` | The vera. |
| `speed` | `number` | The speed. |
| `zomma` | `number` | The zomma. |
| `color` | `string` | The color. |
| `ultima` | `string` | The ultima. |
| `d1` | `number` | The d1. |
| `d2` | `number` | The d2. |
| `dual_delta` | `string` | The dual delta. |
| `dual_gamma` | `number` | The dual gamma. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `number` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## First Order Greeks (option_history_greeks_first_order)

- 文档页: https://docs.thetadata.us/operations/option_history_greeks_first_order.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/greeks/first_order`
- 最低权限级别: `standard`
- 描述: - Returns the data for all contracts that share the same provided symbol and expiration. 
- Calculated using the option and underlying midpoint price. If an interval size is specified (*highly recommended*), the option quote used in the calculation follows the same rules as the [quote](/operations/option_history_quote.html) endpoint. 
- The underlying price represents whatever the last underlying price was at the ``timestamp`` field. You can read more about how Theta Data calculates greeks [here](/Articles/Data-And-Requests/Option-Greeks.html).
- Multi-day requests are limited to 1 month of data.
- 示例调用:
  - Returns first order greeks for an option contract: `http://127.0.0.1:25503/v3/option/history/greeks/first_order?symbol=AAPL&expiration=20241108&date=20241104&interval=5m`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/history/greeks/first_order?symbol=AAPL&expiration=20241108&date=20241104&interval=5m&format=html`
  - Returns first order greeks for an option contract between the specified dates (inclusive): `http://127.0.0.1:25503/v3/option/history/greeks/first_order?symbol=AAPL&expiration=20241108&start_date=20241104&end_date=20241107&interval=5m`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `start_time` | `query` | `string` | `no` | `09:30:00` | `` | The start time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `end_time` | `query` | `string` | `no` | `16:00:00` | `` | The end time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `interval` | `query` | `string` | `yes` | `1s` | `tick, 10ms, 100ms, 500ms, 1s, 5s, 10s, 15s, 30s, 1m, 5m, 10m, 15m, 30m, 1h` | The size of the time interval must be one of the available options listed below. Intervals less than 1m are available only for single-day requests. |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns first order greeks for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `bid` | `number` | The last NBBO bid price. |
| `ask` | `number` | The last NBBO ask price. |
| `delta` | `number` | The delta. |
| `theta` | `number` | The Theta. |
| `vega` | `number` | The vega. |
| `rho` | `number` | The rho. |
| `epsilon` | `number` | The epsilon. |
| `lambda` | `number` | The lambda. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `number` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## Implied Volatility (option_history_greeks_implied_volatility)

- 文档页: https://docs.thetadata.us/operations/option_history_greeks_implied_volatility.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/greeks/implied_volatility`
- 最低权限级别: `standard`
- 描述: - Returns implied volatilies calculated using the national best bid, mid, and ask price of the option respectively. 
- The underlying price represents whatever the last underlying price was at the ``timestamp`` field. You can read more about how Theta Data calculates greeks [here](/Articles/Data-And-Requests/Option-Greeks.html).
- Multi-day requests are limited to 1 month of data.
- 示例调用:
  - Returns 5m interval implied volatility for an option contract: `http://127.0.0.1:25503/v3/option/history/greeks/implied_volatility?symbol=AAPL&expiration=20241108&strike=220.000&right=call&date=20241104&interval=5m`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/history/greeks/implied_volatility?symbol=AAPL&expiration=20241108&strike=220.000&right=call&date=20241104&interval=5m&format=html`
  - Returns 5m interval implied volatility for an option contract between the specified dates (inclusive): `http://127.0.0.1:25503/v3/option/history/greeks/implied_volatility?symbol=AAPL&expiration=20241108&strike=220.000&right=call&start_date=20241104&end_date=20241107&interval=5m`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `start_time` | `query` | `string` | `no` | `09:30:00` | `` | The start time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `end_time` | `query` | `string` | `no` | `16:00:00` | `` | The end time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `interval` | `query` | `string` | `yes` | `1s` | `tick, 10ms, 100ms, 500ms, 1s, 5s, 10s, 15s, 30s, 1m, 5m, 10m, 15m, 30m, 1h` | The size of the time interval must be one of the available options listed below. Intervals less than 1m are available only for single-day requests. |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns 5m interval implied volatility for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `bid` | `number` | The last NBBO bid price. |
| `bid_implied_vol` | `number` | The implied volatiltiy calculated using the bid price. |
| `midpoint` | `number` | The midpoint calculated by averaging the bid & ask prices. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `ask` | `number` | The last NBBO ask price. |
| `ask_implied_vol` | `number` | The implied volatiltiy calculated using the ask price. |
| `iv_error` | `number` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## Second Order Greeks (option_history_greeks_second_order)

- 文档页: https://docs.thetadata.us/operations/option_history_greeks_second_order.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/greeks/second_order`
- 最低权限级别: `professional`
- 描述: - Returns the data for all contracts that share the same provided symbol and expiration. 
- Calculated using the option and underlying midpoint price. If an interval size is specified (*highly recommended*), the option quote used in the calculation follows the same rules as the [quote](/operations/option_history_quote.html) endpoint. 
- The underlying price represents whatever the last underlying price was at the ``timestamp`` field. You can read more about how Theta Data calculates greeks [here](/Articles/Data-And-Requests/Option-Greeks.html).
- Multi-day requests are limited to 1 month of data.
- 示例调用:
  - Returns second order greeks for an option contract: `http://127.0.0.1:25503/v3/option/history/greeks/second_order?symbol=AAPL&expiration=20241108&date=20241104&interval=1h`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/history/greeks/second_order?symbol=AAPL&expiration=20241108&date=20241104&interval=1h&format=html`
  - Returns second order greeks for an option contract between the specified dates (inclusive): `http://127.0.0.1:25503/v3/option/history/greeks/second_order?symbol=AAPL&expiration=20241108&start_date=20241104&end_date=20241107&interval=1h`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `start_time` | `query` | `string` | `no` | `09:30:00` | `` | The start time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `end_time` | `query` | `string` | `no` | `16:00:00` | `` | The end time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `interval` | `query` | `string` | `yes` | `1s` | `tick, 10ms, 100ms, 500ms, 1s, 5s, 10s, 15s, 30s, 1m, 5m, 10m, 15m, 30m, 1h` | The size of the time interval must be one of the available options listed below. Intervals less than 1m are available only for single-day requests. |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns second order greeks for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `bid` | `number` | The last NBBO bid price. |
| `ask` | `number` | The last NBBO ask price. |
| `gamma` | `number` | The gamma. |
| `vanna` | `number` | The vanna. |
| `charm` | `number` | The charm. |
| `vomma` | `number` | The vomma. |
| `veta` | `number` | The veta. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `number` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## Third Order Greeks (option_history_greeks_third_order)

- 文档页: https://docs.thetadata.us/operations/option_history_greeks_third_order.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/greeks/third_order`
- 最低权限级别: `professional`
- 描述: - Returns the data for all contracts that share the same provided symbol and expiration. 
- Calculated using the option and underlying midpoint price. If an interval size is specified (*highly recommended*), the option quote used in the calculation follows the same rules as the [quote](/operations/option_history_quote.html) endpoint. 
- The underlying price represents whatever the last underlying price was at the ``timestamp`` field. You can read more about how Theta Data calculates greeks [here](/Articles/Data-And-Requests/Option-Greeks.html).
- Multi-day requests are limited to 1 month of data.
- 示例调用:
  - Returns third order greeks for an option contract: `http://127.0.0.1:25503/v3/option/history/greeks/third_order?symbol=AAPL&expiration=20241108&date=20241104&interval=1h`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/history/greeks/third_order?symbol=AAPL&expiration=20241108&date=20241104&interval=1h&format=html`
  - Returns third order greeks for an option contract between the specified dates (inclusive): `http://127.0.0.1:25503/v3/option/history/greeks/third_order?symbol=AAPL&expiration=20241108&start_date=20241104&end_date=20241107&interval=1h`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `start_time` | `query` | `string` | `no` | `09:30:00` | `` | The start time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `end_time` | `query` | `string` | `no` | `16:00:00` | `` | The end time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `interval` | `query` | `string` | `yes` | `1s` | `tick, 10ms, 100ms, 500ms, 1s, 5s, 10s, 15s, 30s, 1m, 5m, 10m, 15m, 30m, 1h` | The size of the time interval must be one of the available options listed below. Intervals less than 1m are available only for single-day requests. |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns third order greeks for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `bid` | `number` | The last NBBO bid price. |
| `ask` | `number` | The last NBBO ask price. |
| `speed` | `number` | The speed. |
| `zomma` | `number` | The zomma. |
| `color` | `number` | The color. |
| `ultima` | `number` | The ultima. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `number` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## Open High Low Close (option_history_ohlc)

- 文档页: https://docs.thetadata.us/operations/option_history_ohlc.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/ohlc`
- 最低权限级别: `value`
- 描述: - Aggregated OHLC bars that use [SIP rules](/Articles/Data-And-Requests/OHLC-EOD.html) for each bar. 
- Time timestamp of the bar represents the opening time of the bar. For a trade to be part of the bar:  ``bar timestamp`` <= ``trade time`` < ``bar timestamp + interval``.
- Multi-day requests are limited to 1 month of data.
- 示例调用:
  - Returns OHLC for an option contract: `http://127.0.0.1:25503/v3/option/history/ohlc?symbol=AAPL&expiration=20231103&strike=170.000&right=call&date=20231103&interval=1m`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/history/ohlc?symbol=AAPL&expiration=20231103&strike=170.000&right=call&date=20231103&interval=1m&format=html`
  - Returns OHLC for an option contract: `http://127.0.0.1:25503/v3/option/history/ohlc?symbol=AAPL&expiration=20231103&strike=170.000&right=call&start_date=20231103&end_date=20231110&interval=1m`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `interval` | `query` | `string` | `yes` | `1s` | `tick, 10ms, 100ms, 500ms, 1s, 5s, 10s, 15s, 30s, 1m, 5m, 10m, 15m, 30m, 1h` | The size of the time interval must be one of the available options listed below. Intervals less than 1m are available only for single-day requests. |
| `start_time` | `query` | `string` | `no` | `09:30:00` | `` | The start time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `end_time` | `query` | `string` | `no` | `16:00:00` | `` | The end time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns OHLC for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `open` | `number` | The opening trade price. |
| `high` | `number` | The highest traded price. |
| `low` | `number` | The lowest traded price. |
| `close` | `number` | The closing traded price. |
| `volume` | `integer` | The amount of contracts / shares traded. |
| `count` | `integer` | The amount of trades. |
| `vwap` | `number` | The volume weighted average price of the trading session. |

---

## Open Interest (option_history_open_interest)

- 文档页: https://docs.thetadata.us/operations/option_history_open_interest.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/open_interest`
- 最低权限级别: `value`
- 描述: - Open Interest is normally reported once per day by [OPRA](/Articles/Data-And-Requests/The-SIPs.html) at approximately 06:30 ET.
- A new open interest message might not be sent by [OPRA](/Articles/Data-And-Requests/The-SIPs.html) if there is no open interest for the option contract.
- The reported open interest represents the open interest at the end of the previous trading day.
- 示例调用:
  - Returns open interest for an option contract: `http://127.0.0.1:25503/v3/option/history/open_interest?symbol=AAPL&expiration=20241108&strike=220.000&right=call&date=20241104`
  - Returns open interest for an option contract between the specified dates (inclusive): `http://127.0.0.1:25503/v3/option/history/open_interest?symbol=AAPL&expiration=20241108&strike=220.000&right=call&start_date=20241104&end_date=20241107`
  - Returns open interest for all option contracts: `http://127.0.0.1:25503/v3/option/history/open_interest?symbol=AAPL&expiration=*&date=20241104`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns open interest for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `open_interest` | `integer` | The total amount of outstanding contracts. |

---

## Quote (option_history_quote)

- 文档页: https://docs.thetadata.us/operations/option_history_quote.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/quote`
- 最低权限级别: `value`
- 描述: - Returns every NBBO quote reported by [OPRA](/Articles/Data-And-Requests/The-SIPs.html). 
- If the ``interval`` parameter is specified, the quote for each interval represents the last quote at the interval's timestamp.
- Multi-day requests are limited to 1 month of data, and must specify an expiration.
- 示例调用:
  - Returns every quote for an option contract: `http://127.0.0.1:25503/v3/option/history/quote?symbol=AAPL&expiration=20241108&strike=220.000&right=call&date=20241104&interval=1m`
  - Returns every quote for an option contract in the specified date range: `http://127.0.0.1:25503/v3/option/history/quote?symbol=AAPL&expiration=20241108&strike=220.000&right=call&start_date=20241104&end_date=20241111&interval=1m`
  - Returns every quote for all option contracts: `http://127.0.0.1:25503/v3/option/history/quote?symbol=AAPL&expiration=*&date=20241104&interval=1m`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `start_time` | `query` | `string` | `no` | `09:30:00` | `` | The start time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `end_time` | `query` | `string` | `no` | `16:00:00` | `` | The end time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `interval` | `query` | `string` | `yes` | `1s` | `tick, 10ms, 100ms, 500ms, 1s, 5s, 10s, 15s, 30s, 1m, 5m, 10m, 15m, 30m, 1h` | The size of the time interval must be one of the available options listed below. Intervals less than 1m are available only for single-day requests. |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns every quote for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `bid_size` | `integer` | The last NBBO bid size. |
| `bid_exchange` | `integer` | The last NBBO bid [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html). |
| `bid` | `number` | The last NBBO bid price. |
| `bid_condition` | `integer` | The last NBBO bid [condition](/Articles/Errors-Exchanges-Conditions/Quote-Conditions.html). |
| `ask_size` | `integer` | The last NBBO ask size. |
| `ask_exchange` | `integer` | The last NBBO ask [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html). |
| `ask` | `number` | The last NBBO ask price. |
| `ask_condition` | `integer` | The last NBBO ask [condition](/Articles/Errors-Exchanges-Conditions/Quote-Conditions.html). |

---

## Trade (option_history_trade)

- 文档页: https://docs.thetadata.us/operations/option_history_trade.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/trade`
- 最低权限级别: `standard`
- 描述: - Returns every trade reported by [OPRA](/Articles/Data-And-Requests/The-SIPs.html). 
- Trade condition mappings can be found [here](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html).
- Extended trade conditions are not reported by [OPRA](/Articles/Data-And-Requests/The-SIPs.html) for options, so they can be ignored.
- Multi-day requests are limited to 1 month of data, and must specify an expiration.
- 示例调用:
  - Returns every trade for an option contract: `http://127.0.0.1:25503/v3/option/history/trade?symbol=AAPL&expiration=20241108&strike=220.000&right=call&date=20241104`
  - Returns every trade for an option contract between the specified dates (inclusive): `http://127.0.0.1:25503/v3/option/history/trade?symbol=AAPL&expiration=20241108&strike=220.000&right=call&start_date=20241104&end_date=20241110`
  - Returns every trade for all option contracts: `http://127.0.0.1:25503/v3/option/history/trade?symbol=AAPL&expiration=*&date=20241104`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `start_time` | `query` | `string` | `no` | `09:30:00` | `` | The start time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `end_time` | `query` | `string` | `no` | `16:00:00` | `` | The end time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns every trade for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `sequence` | `integer` | The exchange [sequence](/Articles/Data-And-Requests/Making-Requests.html#trade-sequences). |
| `ext_condition1` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition2` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition3` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition4` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `condition` | `integer` | The trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html). |
| `size` | `integer` | The amount of contracts / shares traded. |
| `exchange` | `integer` | The [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html) the trade was executed. |
| `price` | `number` | The trade price. |

---

## All Trade Greeks (option_history_trade_greeks_all)

- 文档页: https://docs.thetadata.us/operations/option_history_trade_greeks_all.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/trade_greeks/all`
- 最低权限级别: `professional`
- 描述: - Returns the data for all contracts that share the same provided symbol and expiration. 
- Calculates greeks for every trade reported by [OPRA](/Articles/Data-And-Requests/The-SIPs.html).
- The underlying price represents whatever the last underlying price was at the ``timestamp`` field. You can read more about how Theta Data calculates greeks [here](/Articles/Data-And-Requests/Option-Greeks.html).
- Multi-day requests are limited to 1 month of data, and must specify an expiration.
- 示例调用:
  - Returns all trade greeks for an option contract: `http://127.0.0.1:25503/v3/option/history/trade_greeks/all?symbol=AAPL&expiration=20231117&date=20231110`
  - Returns all trade greeks for an option contract between the specified dates (inclusive): `http://127.0.0.1:25503/v3/option/history/trade_greeks/all?symbol=AAPL&expiration=20231117&start_date=20231110&end_date=20231116`
  - Returns all trade greeks for an full chain of option contracts: `http://127.0.0.1:25503/v3/option/history/trade_greeks/all?symbol=AAPL&expiration=*&date=20231110`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `start_time` | `query` | `string` | `no` | `09:30:00` | `` | The start time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `end_time` | `query` | `string` | `no` | `16:00:00` | `` | The end time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns all trade greeks for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `sequence` | `string` | The exchange [sequence](/Articles/Data-And-Requests/Making-Requests.html#trade-sequences). |
| `ext_condition1` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition2` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition3` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition4` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `condition` | `integer` | The trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html). |
| `size` | `integer` | The amount of contracts / shares traded. |
| `exchange` | `integer` | The [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html) the trade was executed. |
| `price` | `number` | The trade price. |
| `delta` | `number` | The delta. |
| `theta` | `string` | The Theta. |
| `vega` | `number` | The vega. |
| `rho` | `number` | The rho. |
| `epsilon` | `string` | The epsilon. |
| `lambda` | `number` | The lambda. |
| `gamma` | `number` | The gamma. |
| `vanna` | `number` | The vanna. |
| `charm` | `string` | The charm. |
| `vomma` | `number` | The vomma. |
| `veta` | `number` | The veta. |
| `vera` | `number` | The vera. |
| `speed` | `number` | The speed. |
| `zomma` | `number` | The zomma. |
| `color` | `string` | The color. |
| `ultima` | `string` | The ultima. |
| `d1` | `string` | The d1. |
| `d2` | `string` | The d2. |
| `dual_delta` | `string` | The dual delta. |
| `dual_gamma` | `number` | The dual gamma. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `string` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## First Order Trade Greeks (option_history_trade_greeks_first_order)

- 文档页: https://docs.thetadata.us/operations/option_history_trade_greeks_first_order.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/trade_greeks/first_order`
- 最低权限级别: `professional`
- 描述: - Returns the data for all contracts that share the same provided symbol and expiration.
- Calculates greeks for every trade reported by [OPRA](/Articles/Data-And-Requests/The-SIPs.html).
- The underlying price represents whatever the last underlying price was at the ``timestamp`` field. You can read more about how Theta Data calculates greeks [here](/Articles/Data-And-Requests/Option-Greeks.html).
- Multi-day requests are limited to 1 month of data, and must specify an expiration.
- 示例调用:
  - Returns first order trade greeks for an option contract: `http://127.0.0.1:25503/v3/option/history/trade_greeks/first_order?symbol=AAPL&expiration=20241108&date=20241104`
  - Returns first order trade greeks for an option contract between the specified dates (inclusive): `http://127.0.0.1:25503/v3/option/history/trade_greeks/first_order?symbol=AAPL&expiration=20241108&start_date=20241104&end_date=20241110`
  - Returns first order trade greeks for an full chain of option contracts: `http://127.0.0.1:25503/v3/option/history/trade_greeks/first_order?symbol=AAPL&expiration=*&date=20241104`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `start_time` | `query` | `string` | `no` | `09:30:00` | `` | The start time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `end_time` | `query` | `string` | `no` | `16:00:00` | `` | The end time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns first order trade greeks for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `sequence` | `integer` | The exchange [sequence](/Articles/Data-And-Requests/Making-Requests.html#trade-sequences). |
| `ext_condition1` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition2` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition3` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition4` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `condition` | `integer` | The trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html). |
| `size` | `integer` | The amount of contracts / shares traded. |
| `exchange` | `integer` | The [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html) the trade was executed. |
| `price` | `number` | The trade price. |
| `delta` | `number` | The delta. |
| `theta` | `string` | The Theta. |
| `vega` | `number` | The vega. |
| `rho` | `number` | The rho. |
| `epsilon` | `string` | The epsilon. |
| `lambda` | `number` | The lambda. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `number` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## Trade Implied Volatility (option_history_trade_greeks_implied_volatility)

- 文档页: https://docs.thetadata.us/operations/option_history_trade_greeks_implied_volatility.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/trade_greeks/implied_volatility`
- 最低权限级别: `professional`
- 描述: - Returns implied volatilies calculated using the trade reported by [OPRA](/Articles/Data-And-Requests/The-SIPs.html). 
- The underlying price represents whatever the last underlying price was at the ``timestamp`` field. You can read more about how Theta Data calculates greeks [here](/Articles/Data-And-Requests/Option-Greeks.html).
- Multi-day requests are limited to 1 month of data, and must specify an expiration.
- 示例调用:
  - Returns implied volatility for an option contract: `http://127.0.0.1:25503/v3/option/history/trade_greeks/implied_volatility?symbol=AAPL&expiration=20241108&strike=220.000&right=call&date=20241104`
  - Returns implied volatility for an option contract between the specified dates (inclusive): `http://127.0.0.1:25503/v3/option/history/trade_greeks/implied_volatility?symbol=AAPL&expiration=20241108&strike=220.000&right=call&start_date=20241104&end_date=20241108`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/history/trade_greeks/implied_volatility?symbol=AAPL&expiration=20241108&strike=220.000&right=call&date=20241104&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `start_time` | `query` | `string` | `no` | `09:30:00` | `` | The start time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `end_time` | `query` | `string` | `no` | `16:00:00` | `` | The end time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns implied volatility for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `sequence` | `integer` | The exchange [sequence](/Articles/Data-And-Requests/Making-Requests.html#trade-sequences). |
| `ext_condition1` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition2` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition3` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition4` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `condition` | `integer` | The trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html). |
| `size` | `integer` | The amount of contracts / shares traded. |
| `exchange` | `integer` | The [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html) the trade was executed. |
| `price` | `number` | The trade price. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `number` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## Second Order Trade Greeks (option_history_trade_greeks_second_order)

- 文档页: https://docs.thetadata.us/operations/option_history_trade_greeks_second_order.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/trade_greeks/second_order`
- 最低权限级别: `professional`
- 描述: - Returns the data for all contracts that share the same provided symbol and expiration.
- Calculates greeks for every trade reported by [OPRA](/Articles/Data-And-Requests/The-SIPs.html).
- The underlying price represents whatever the last underlying price was at the ``timestamp`` field. You can read more about how Theta Data calculates greeks [here](/Articles/Data-And-Requests/Option-Greeks.html).
- Multi-day requests are limited to 1 month of data, and must specify an expiration.
- 示例调用:
  - Returns second order trade greeks for an option contract: `http://127.0.0.1:25503/v3/option/history/trade_greeks/second_order?symbol=AAPL&expiration=20241108&date=20241104`
  - Returns second order trade greeks for an option contract between the specified dates (inclusive): `http://127.0.0.1:25503/v3/option/history/trade_greeks/second_order?symbol=AAPL&expiration=20241108&start_date=20241104&end_date=20241110`
  - Returns second order trade greeks for an full chain of option contracts: `http://127.0.0.1:25503/v3/option/history/trade_greeks/second_order?symbol=AAPL&expiration=*&date=20241104`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `start_time` | `query` | `string` | `no` | `09:30:00` | `` | The start time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `end_time` | `query` | `string` | `no` | `16:00:00` | `` | The end time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns second order trade greeks for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `sequence` | `integer` | The exchange [sequence](/Articles/Data-And-Requests/Making-Requests.html#trade-sequences). |
| `ext_condition1` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition2` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition3` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition4` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `condition` | `integer` | The trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html). |
| `size` | `integer` | The amount of contracts / shares traded. |
| `exchange` | `integer` | The [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html) the trade was executed. |
| `price` | `number` | The trade price. |
| `gamma` | `number` | The gamma. |
| `vanna` | `number` | The vanna. |
| `charm` | `string` | The charm. |
| `vomma` | `number` | The vomma. |
| `veta` | `number` | The veta. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `number` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## Third Order Trade Greeks (option_history_trade_greeks_third_order)

- 文档页: https://docs.thetadata.us/operations/option_history_trade_greeks_third_order.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/trade_greeks/third_order`
- 最低权限级别: `professional`
- 描述: - Returns the data for all contracts that share the same provided symbol and expiration.
- Calculates greeks for every trade reported by [OPRA](/Articles/Data-And-Requests/The-SIPs.html).
- The underlying price represents whatever the last underlying price was at the ``timestamp`` field. You can read more about how Theta Data calculates greeks [here](/Articles/Data-And-Requests/Option-Greeks.html).
- Multi-day requests are limited to 1 month of data, and must specify an expiration.
- 示例调用:
  - Returns third order trade greeks for an option contract: `http://127.0.0.1:25503/v3/option/history/trade_greeks/third_order?symbol=AAPL&expiration=20241108&date=20241104`
  - Returns third order trade greeks for an option contract between the specified dates (inclusive): `http://127.0.0.1:25503/v3/option/history/trade_greeks/third_order?symbol=AAPL&expiration=20241108&start_date=20241104&end_date=20241110`
  - Returns third order trade greeks for an full chain of option contracts: `http://127.0.0.1:25503/v3/option/history/trade_greeks/third_order?symbol=AAPL&expiration=*&date=20241104`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `start_time` | `query` | `string` | `no` | `09:30:00` | `` | The start time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `end_time` | `query` | `string` | `no` | `16:00:00` | `` | The end time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns third order trade greeks for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `sequence` | `integer` | The exchange [sequence](/Articles/Data-And-Requests/Making-Requests.html#trade-sequences). |
| `ext_condition1` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition2` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition3` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition4` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `condition` | `integer` | The trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html). |
| `size` | `integer` | The amount of contracts / shares traded. |
| `exchange` | `integer` | The [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html) the trade was executed. |
| `price` | `number` | The trade price. |
| `speed` | `number` | The speed. |
| `zomma` | `number` | The zomma. |
| `color` | `string` | The color. |
| `ultima` | `number` | The ultima. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `number` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## Trade Quote (option_history_trade_quote)

- 文档页: https://docs.thetadata.us/operations/option_history_trade_quote.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/history/trade_quote`
- 最低权限级别: `standard`
- 描述: - Returns every [trade](/operations/option_history_trade.html) reported by [OPRA](/Articles/Data-And-Requests/The-SIPs.html) paired with the last NBBO quote reported by [OPRA](/Articles/Data-And-Requests/The-SIPs.html) at the time of trade.
- A quote is matched with a trade if its timestamp ``<=`` the trade timestamp. 
- To match trades with quotes timestamps that are ``<`` the trade timestamp, specify the ``exclusive``parameter to ``true``. After thorough testing, we have determined that using ``exclusive=true`` might yield better results for various applications.
- Multi-day requests are limited to 1 month of data, and must specify an expiration.
- 示例调用:
  - Returns every trade quote for an option contract: `http://127.0.0.1:25503/v3/option/history/trade_quote?symbol=AAPL&expiration=20241108&strike=220.000&right=call&date=20241104`
  - Returns every trade quote for an option contract between the specified dates (inclusive): `http://127.0.0.1:25503/v3/option/history/trade_quote?symbol=AAPL&expiration=20241108&strike=220.000&right=call&start_date=20241104&end_date=20241110`
  - Returns every trade quote for all option contracts: `http://127.0.0.1:25503/v3/option/history/trade_quote?symbol=AAPL&expiration=*&date=20241104`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `date` | `query` | `string` | `no` | `` | `` | The date to fetch data for. If present, this overrides start_date and end_date. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `start_time` | `query` | `string` | `no` | `09:30:00` | `` | The start time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `end_time` | `query` | `string` | `no` | `16:00:00` | `` | The end time (inclusive) in the specified day (format 24-hour HH:MM:SS.SSS). |
| `exclusive` | `query` | `boolean` | `no` | `True` | `` | If you prefer to match quotes with timestamps that are < the trade timestamp. |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |
| `start_date` | `query` | `string` | `no` | `` | `` | The start date (inclusive). |
| `end_date` | `query` | `string` | `no` | `` | `` | The end date (inclusive). |

### 输出 (200)

- 描述: Returns every trade quote for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `trade_timestamp` | `string` | The trade date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `quote_timestamp` | `string` | The quote date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `sequence` | `integer` | The exchange [sequence](/Articles/Data-And-Requests/Making-Requests.html#trade-sequences). |
| `ext_condition1` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition2` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition3` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition4` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `condition` | `integer` | The trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html). |
| `size` | `integer` | The amount of contracts / shares traded. |
| `exchange` | `integer` | The [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html) the trade was executed. |
| `price` | `number` | The trade price. |
| `bid_size` | `integer` | The last NBBO bid size. |
| `bid_exchange` | `integer` | The last NBBO bid [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html). |
| `bid` | `number` | The last NBBO bid price. |
| `bid_condition` | `integer` | The last NBBO bid [condition](/Articles/Errors-Exchanges-Conditions/Quote-Conditions.html). |
| `ask_size` | `integer` | The last NBBO ask size. |
| `ask_exchange` | `integer` | The last NBBO ask [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html). |
| `ask` | `number` | The last NBBO ask price. |
| `ask_condition` | `integer` | The last NBBO ask [condition](/Articles/Errors-Exchanges-Conditions/Quote-Conditions.html). |

---

## Contracts (option_list_contracts)

- 文档页: https://docs.thetadata.us/operations/option_list_contracts.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/list/contracts/{request_type}`
- 最低权限级别: `value`
- 描述: Lists all contracts that were traded or quoted on a particular date.

If the ``symbol`` parameter is specified, the returned contracts will be filtered to match the symbol.
Multiple symbols can be specified by separating them with commas such as ``symbol=AAPL,SPY,AMD``
This endpoint is updated real-time.
- 示例调用:
  - List all contracts for an option trade with a given date: `http://127.0.0.1:25503/v3/option/list/contracts/trade?date=20220930`
  - List all contracts for an option quote with a given symbol and date: `http://127.0.0.1:25503/v3/option/list/contracts/quote?symbol=AAPL&date=20220930`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/list/contracts/quote?symbol=AAPL&date=20220930&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `request_type` | `path` | `string` | `yes` | `` | `trade, quote` | The request type. |
| `symbol` | `query` | `array<string>` | `no` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `date` | `query` | `string` | `yes` | `` | `` | The date to fetch data for. |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: List all contracts for an option trade with a given date
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |

---

## Dates (option_list_dates)

- 文档页: https://docs.thetadata.us/operations/option_list_dates.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/list/dates/{request_type}`
- 最低权限级别: `free`
- 描述: Lists all dates of data that are available for an option with a given symbol, request type, and expiration.
This endpoint is updated overnight.
- 示例调用:
  - List all dates for an option quote for a given symbol and expiration date: `http://127.0.0.1:25503/v3/option/list/dates/quote?symbol=AAPL&expiration=20220930`
  - List all dates for an option trade for a given symbol with any expiration date: `http://127.0.0.1:25503/v3/option/list/dates/trade?symbol=AAPL&expiration=20220930`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/list/dates/trade?symbol=AAPL&expiration=20220930&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `request_type` | `path` | `string` | `yes` | `` | `trade, quote` | The request type. |
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: List all dates for an option quote for a given symbol and expiration date
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `date` | `string` | The date formated as YYYY-MM-DD. |

---

## Expirations (option_list_expirations)

- 文档页: https://docs.thetadata.us/operations/option_list_expirations.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/list/expirations`
- 最低权限级别: `free`
- 描述: Lists all dates of expirations that are available for an option with a given symbol.
This endpoint is updated overnight.
- 示例调用:
  - List all expirations for an option with a given symbol: `http://127.0.0.1:25503/v3/option/list/expirations?symbol=AAPL`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/list/expirations?symbol=AAPL&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `array<string>` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. Specify '*' for all symbols or a comma separated list when appropriate. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: List all expirations for an option with a given symbol
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |

---

## Strikes (option_list_strikes)

- 文档页: https://docs.thetadata.us/operations/option_list_strikes.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/list/strikes`
- 最低权限级别: `free`
- 描述: Lists all strikes that are available for an option with a given symbol and expiration date.
This endpoint is updated overnight.
- 示例调用:
  - List all strikes for an option with a given symbol and expiration date: `http://127.0.0.1:25503/v3/option/list/strikes?symbol=AAPL&expiration=20220930`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/list/strikes?symbol=AAPL&expiration=20220930&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `array<string>` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. Specify '*' for all symbols or a comma separated list when appropriate. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format. |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: List all strikes for an option with a given symbol and expiration date
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |

---

## Symbols (option_list_symbols)

- 文档页: https://docs.thetadata.us/operations/option_list_symbols.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/list/symbols`
- 最低权限级别: `free`
- 描述: A symbol can be defined as a unique identifier for a stock / underlying asset. Common terms also include: root, ticker, and underlying. This endpoint returns all traded symbols for options. This endpoint is updated overnight.
- 示例调用:
  - List all symbols for options: `http://127.0.0.1:25503/v3/option/list/symbols`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/list/symbols?format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: List all symbols for options
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |

---

## All Greeks (option_snapshot_greeks_all)

- 文档页: https://docs.thetadata.us/operations/option_snapshot_greeks_all.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/snapshot/greeks/all`
- 最低权限级别: `professional`
- 描述: - Retrieve a real-time last greeks calculation for all option contracts that lie on a provided expiration.
- You might need to change the default expiration date to a different date if it is past the current date. Some quotes are omitted in the example to reduce the space of the sample output.
- Make `expiration` * if you want to get the snapshot for every expiration chain for the underlying.
> This endpoint will return no data if the market was closed for the day. Theta Data resets the snapshot cache at midnight ET every night.
- 示例调用:
  - Returns all greeks for an option contract: `http://127.0.0.1:25503/v3/option/snapshot/greeks/all?symbol=AAPL&expiration=2026-05-15&strike=170.00&right=call`
  - Returns all greeks for all option contracts: `http://127.0.0.1:25503/v3/option/snapshot/greeks/all?symbol=AAPL&expiration=*`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/snapshot/greeks/all?symbol=AAPL&expiration=*&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `stock_price` | `query` | `number` | `no` | `` | `` | The underlying stock price to be used in the Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `min_time` | `query` | `string` | `no` | `` | `` | Filters snapshots to include only data with a timestamp greater or equal to the specified value (HH:mm:ss.SSS format). |
| `use_market_value` | `query` | `boolean` | `no` | `False` | `` | Use the market value bid, ask, and price |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: Returns all greeks for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `bid` | `number` | The last NBBO bid price. |
| `ask` | `number` | The last NBBO ask price. |
| `delta` | `number` | The delta. |
| `theta` | `string` | The Theta. |
| `vega` | `number` | The vega. |
| `rho` | `number` | The rho. |
| `epsilon` | `string` | The epsilon. |
| `lambda` | `number` | The lambda. |
| `gamma` | `number` | The gamma. |
| `vanna` | `string` | The vanna. |
| `charm` | `number` | The charm. |
| `vomma` | `number` | The vomma. |
| `veta` | `number` | The veta. |
| `vera` | `number` | The vera. |
| `speed` | `number` | The speed. |
| `zomma` | `number` | The zomma. |
| `color` | `string` | The color. |
| `ultima` | `string` | The ultima. |
| `d1` | `number` | The d1. |
| `d2` | `number` | The d2. |
| `dual_delta` | `string` | The dual delta. |
| `dual_gamma` | `number` | The dual gamma. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `number` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## First Order Greeks (option_snapshot_greeks_first_order)

- 文档页: https://docs.thetadata.us/operations/option_snapshot_greeks_first_order.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/snapshot/greeks/first_order`
- 最低权限级别: `standard`
- 描述: - Retrieve a real-time last greeks calculation for all option contracts that lie on a provided expiration.
- You might need to change the default expiration date to a different date if it is past the current date. Some quotes are omitted in the example to reduce the space of the sample output.
- Make `expiration` * if you want to get the snapshot for every expiration chain for the underlying.
> This endpoint will return no data if the market was closed for the day. Theta Data resets the snapshot cache at midnight ET every night.
- 示例调用:
  - Returns first order greeks for an option contract: `http://127.0.0.1:25503/v3/option/snapshot/greeks/first_order?symbol=AAPL&expiration=20270115&strike=270.000&right=call`
  - Returns first order greeks for all option contracts: `http://127.0.0.1:25503/v3/option/snapshot/greeks/first_order?symbol=AAPL&expiration=*`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/snapshot/greeks/first_order?symbol=AAPL&expiration=*&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `stock_price` | `query` | `number` | `no` | `` | `` | The underlying stock price to be used in the Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `min_time` | `query` | `string` | `no` | `` | `` | Filters snapshots to include only data with a timestamp greater or equal to the specified value (HH:mm:ss.SSS format). |
| `use_market_value` | `query` | `boolean` | `no` | `False` | `` | Use the market value bid, ask, and price |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: Returns first order greeks for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `bid` | `number` | The last NBBO bid price. |
| `ask` | `number` | The last NBBO ask price. |
| `delta` | `number` | The delta. |
| `theta` | `string` | The Theta. |
| `vega` | `number` | The vega. |
| `rho` | `number` | The rho. |
| `epsilon` | `string` | The epsilon. |
| `lambda` | `number` | The lambda. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `string` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## Implied Volatility (option_snapshot_greeks_implied_volatility)

- 文档页: https://docs.thetadata.us/operations/option_snapshot_greeks_implied_volatility.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/snapshot/greeks/implied_volatility`
- 最低权限级别: `standard`
- 描述: Returns implied volatilies calculated using the national best bid, mid, and ask price
of the option respectively. The underlying price represents whatever the last underlying price was at the
``underlying_timestamp`` field. You can read more about how Theta Data calculates greeks 
[here](/Articles/Data-And-Requests/Option-Greeks.html).
- 示例调用:
  - Returns implied volatility for an option contract: `http://127.0.0.1:25503/v3/option/snapshot/greeks/implied_volatility?symbol=AAPL&expiration=20270115&strike=270.000&right=call`
  - Returns implied volatility for all option contracts: `http://127.0.0.1:25503/v3/option/snapshot/greeks/implied_volatility?symbol=AAPL&expiration=*`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/snapshot/greeks/implied_volatility?symbol=AAPL&expiration=*&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `stock_price` | `query` | `number` | `no` | `` | `` | The underlying stock price to be used in the Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `min_time` | `query` | `string` | `no` | `` | `` | Filters snapshots to include only data with a timestamp greater or equal to the specified value (HH:mm:ss.SSS format). |
| `use_market_value` | `query` | `boolean` | `no` | `False` | `` | Use the market value bid, ask, and price |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: Returns implied volatility for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `bid` | `number` | The last NBBO bid price. |
| `ask` | `number` | The last NBBO ask price. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `string` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## Second Order Greeks (option_snapshot_greeks_second_order)

- 文档页: https://docs.thetadata.us/operations/option_snapshot_greeks_second_order.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/snapshot/greeks/second_order`
- 最低权限级别: `professional`
- 描述: - Retrieve a real-time last second order greeks calculation for all option contracts that lie on a provided expiration.
- You might need to change the default expiration date to a different date if it is past the current date. Some quotes are omitted in the example to reduce the space of the sample output.
- Make `expiration` * if you want to get the snapshot for every expiration chain for the underlying.
> This endpoint will return no data if the market was closed for the day. Theta Data resets the snapshot cache at midnight ET every night.
- 示例调用:
  - Returns second order greeks for an option contract: `http://127.0.0.1:25503/v3/option/snapshot/greeks/second_order?symbol=AAPL&expiration=20270115&strike=270.00`
  - Returns second order greeks for all option contracts: `http://127.0.0.1:25503/v3/option/snapshot/greeks/second_order?symbol=AAPL&expiration=*`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/snapshot/greeks/second_order?symbol=AAPL&expiration=*&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `stock_price` | `query` | `number` | `no` | `` | `` | The underlying stock price to be used in the Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `min_time` | `query` | `string` | `no` | `` | `` | Filters snapshots to include only data with a timestamp greater or equal to the specified value (HH:mm:ss.SSS format). |
| `use_market_value` | `query` | `boolean` | `no` | `False` | `` | Use the market value bid, ask, and price |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: Returns second order greeks for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `bid` | `number` | The last NBBO bid price. |
| `ask` | `number` | The last NBBO ask price. |
| `gamma` | `number` | The gamma. |
| `vanna` | `number` | The vanna. |
| `charm` | `string` | The charm. |
| `vomma` | `number` | The vomma. |
| `veta` | `number` | The veta. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `string` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## Third Order Greeks (option_snapshot_greeks_third_order)

- 文档页: https://docs.thetadata.us/operations/option_snapshot_greeks_third_order.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/snapshot/greeks/third_order`
- 最低权限级别: `professional`
- 描述: - Retrieve a real-time last third order greeks calculation for all option contracts that lie on a provided expiration.
- You might need to change the default expiration date to a different date if it is past the current date. Some quotes are omitted in the example to reduce the space of the sample output.
- Make `expiration` * if you want to get the snapshot for every expiration chain for the underlying.
> This endpoint will return no data if the market was closed for the day. Theta Data resets the snapshot cache at midnight ET every night.
- 示例调用:
  - Returns third order greeks for an option contract: `http://127.0.0.1:25503/v3/option/snapshot/greeks/third_order?symbol=AAPL&expiration=20270115&strike=270.00`
  - Returns third order greeks for all option contracts: `http://127.0.0.1:25503/v3/option/snapshot/greeks/third_order?symbol=AAPL&expiration=*`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/snapshot/greeks/third_order?symbol=AAPL&expiration=*&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `annual_dividend` | `query` | `number` | `no` | `` | `` | The annualized expected dividend amount to be used in Greeks calculations. |
| `rate_type` | `query` | `string` | `no` | `sofr` | `sofr, treasury_m1, treasury_m3, treasury_m6, treasury_y1, treasury_y2, treasury_y3, treasury_y5, treasury_y7, treasury_y10, treasury_y20, treasury_y30` | The interest rate type to be used in a Greeks calculation. |
| `rate_value` | `query` | `number` | `no` | `` | `` | The interest rate, as a percent, to be used in a Greeks calculation. |
| `stock_price` | `query` | `number` | `no` | `` | `` | The underlying stock price to be used in the Greeks calculation. |
| `version` | `query` | `string` | `no` | `latest` | `latest, 1` | Used to adjust Greeks calculation methodology. "1" uses a fixed .15 DTE for 0DTE; "latest" uses real TTE (down to a minimum of 1 hour) |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `min_time` | `query` | `string` | `no` | `` | `` | Filters snapshots to include only data with a timestamp greater or equal to the specified value (HH:mm:ss.SSS format). |
| `use_market_value` | `query` | `boolean` | `no` | `False` | `` | Use the market value bid, ask, and price |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: Returns third order greeks for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `bid` | `number` | The last NBBO bid price. |
| `ask` | `number` | The last NBBO ask price. |
| `speed` | `number` | The speed. |
| `zomma` | `number` | The zomma. |
| `color` | `string` | The color. |
| `ultima` | `string` | The ultima. |
| `implied_vol` | `number` | The implied volatiltiy calculated using the trade price. |
| `iv_error` | `string` | IV Error: the value of the option calculated using the implied volatiltiy divided by the actual value reported in the quote. This value will increase as the strike price recedes from the underlying price. |
| `underlying_timestamp` | `string` | The underlying date formated as YYYY-MM-DDTHH:mm:ss.SSS format. |
| `underlying_price` | `number` | The midpoint of the underlying at the time of the option trade. |

---

## Market Value (option_snapshot_market_value)

- 文档页: https://docs.thetadata.us/operations/option_snapshot_market_value.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/snapshot/market_value`
- 最低权限级别: `standard`
- 描述: * Returns a real-time market value derived from the last NBBO quote of an option contract.
- 示例调用:
  - Returns last market value for all option contracts for a given symbol: `http://127.0.0.1:25503/v3/option/snapshot/market_value?symbol=AAPL&expiration=*`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/snapshot/market_value?symbol=AAPL&expiration=*&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `min_time` | `query` | `string` | `no` | `` | `` | Filters snapshots to include only data with a timestamp greater or equal to the specified value (HH:mm:ss.SSS format). |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: Returns last market value for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `market_bid` | `number` | The last market bid |
| `market_ask` | `number` | The last market ask |
| `market_price` | `number` | The last market value price |

---

## Open High Low Close (option_snapshot_ohlc)

- 文档页: https://docs.thetadata.us/operations/option_snapshot_ohlc.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/snapshot/ohlc`
- 最低权限级别: `value`
- 描述: - Retrieve a real-time last ohlc of an option contract for the trading day.
- You might need to change the default expiration date to a different date if it is past the current date.
- 示例调用:
  - Returns OHLC for a given option contract: `http://127.0.0.1:25503/v3/option/snapshot/ohlc?symbol=AAPL&expiration=20270115&right=call&strike=270.000`
  - Returns OHLC for all option contracts: `http://127.0.0.1:25503/v3/option/snapshot/ohlc?symbol=AAPL&expiration=*`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/snapshot/ohlc?symbol=AAPL&expiration=*&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `min_time` | `query` | `string` | `no` | `` | `` | Filters snapshots to include only data with a timestamp greater or equal to the specified value (HH:mm:ss.SSS format). |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: Returns OHLC for a given option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `open` | `number` | The opening trade price. |
| `high` | `number` | The highest traded price. |
| `low` | `number` | The lowest traded price. |
| `close` | `number` | The closing traded price. |
| `volume` | `integer` | The amount of contracts / shares traded. |
| `count` | `integer` | The amount of trades. |

---

## Open Interest (option_snapshot_open_interest)

- 文档页: https://docs.thetadata.us/operations/option_snapshot_open_interest.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/snapshot/open_interest`
- 最低权限级别: `value`
- 描述: - Retrieve the last open interest message of an option contract.
- Open interest is reported around 06:30 ET every morning by OPRA and reflects the open interest at the of the previous trading day. 
- You might need to change the default expiration date to a different date if it is past the current date.
- This endpoint will return no data if the market was closed for the day. Theta Data resets the snapshot cache at midnight ET every night.
- 示例调用:
  - Returns open interest for an option contract: `http://127.0.0.1:25503/v3/option/snapshot/open_interest?symbol=AAPL&expiration=20270115&right=call&strike=270.00`
  - Returns open interest for all option contracts: `http://127.0.0.1:25503/v3/option/snapshot/open_interest?symbol=AAPL&expiration=*`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/snapshot/open_interest?symbol=AAPL&expiration=*&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `min_time` | `query` | `string` | `no` | `` | `` | Filters snapshots to include only data with a timestamp greater or equal to the specified value (HH:mm:ss.SSS format). |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: Returns open interest for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `open_interest` | `integer` | The total amount of outstanding contracts. |

---

## Quote (option_snapshot_quote)

- 文档页: https://docs.thetadata.us/operations/option_snapshot_quote.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/snapshot/quote`
- 最低权限级别: `value`
- 描述: - Retrieve a real-time last NBBO quote of an option contract.
- You might need to change the default expiration date to a different date if it is past the current date.
- This endpoint will return no data if the market was closed for the day. Theta Data resets the snapshot cache at midnight ET every night.
- 示例调用:
  - Returns last NBBO quote for an option contract: `http://127.0.0.1:25503/v3/option/snapshot/quote?symbol=AAPL&expiration=20270115&right=call&strike=270.000`
  - Returns last NBBO quote for all option contracts: `http://127.0.0.1:25503/v3/option/snapshot/quote?symbol=AAPL&expiration=*`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/snapshot/quote?symbol=AAPL&expiration=*&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format, or `*` for all expirations. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `max_dte` | `query` | `integer` | `no` | `` | `` | If specified, only contracts with a full calendar day 'Days to Expiration' (DTE) less than or equal to this number will be returned. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `min_time` | `query` | `string` | `no` | `` | `` | Filters snapshots to include only data with a timestamp greater or equal to the specified value (HH:mm:ss.SSS format). |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: Returns last NBBO quote for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `bid_size` | `integer` | The last NBBO bid size. |
| `bid_exchange` | `integer` | The last NBBO bid [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html). |
| `bid` | `number` | The last NBBO bid price. |
| `bid_condition` | `integer` | The last NBBO bid [condition](/Articles/Errors-Exchanges-Conditions/Quote-Conditions.html). |
| `ask_size` | `integer` | The last NBBO ask size. |
| `ask_exchange` | `integer` | The last NBBO ask [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html). |
| `ask` | `number` | The last NBBO ask price. |
| `ask_condition` | `integer` | The last NBBO ask [condition](/Articles/Errors-Exchanges-Conditions/Quote-Conditions.html). |

---

## Trade (option_snapshot_trade)

- 文档页: https://docs.thetadata.us/operations/option_snapshot_trade.html
- 方法: `GET`
- 地址: `http://127.0.0.1:25503/v3/option/snapshot/trade`
- 最低权限级别: `standard`
- 描述: - Retrieve the real-time last trade of an option contract.
- You might need to change the default expiration date to a different date if it is past the current date.
- This endpoint will return no data if the market was closed for the day. Theta Data resets the snapshot cache at midnight ET every night.
- 示例调用:
  - Returns last trade for an option contract: `http://127.0.0.1:25503/v3/option/snapshot/trade?symbol=AAPL&expiration=2027-01-15&right=call&strike=270.000`
  - Returns last trade for all option contracts with an expiration of 2027-01-15: `http://127.0.0.1:25503/v3/option/snapshot/trade?symbol=AAPL&expiration=2027-01-15`
  - Click to open in browser (HTML): `http://127.0.0.1:25503/v3/option/snapshot/trade?symbol=AAPL&expiration=2027-01-15&format=html`

### 输入参数

| 参数 | 位置 | 类型 | 必填 | 默认值 | 枚举 | 说明 |
|---|---|---|---|---|---|---|
| `symbol` | `query` | `string` | `yes` | `` | `` | The stock or index symbol, or underlying symbol for options. |
| `expiration` | `query` | `string` | `yes` | `` | `` | The expiration of the contract in `YYYY-MM-DD` or `YYYYMMDD` format. |
| `strike` | `query` | `string` | `no` | `*` | `` | The strike price of the contract in dollars (ie `100.00` for `$100.00`), or `*` for all strikes. |
| `right` | `query` | `string` | `no` | `both` | `call, put, both` | The right (call or put) of the contract. |
| `strike_range` | `query` | `integer` | `no` | `` | `` | Used to specify a filter to limit the number of contracts returned relative to the underlying's spot price. Will return the specified number of strikes above and below the spot price, as well as the at-the-money strike. |
| `min_time` | `query` | `string` | `no` | `` | `` | Filters snapshots to include only data with a timestamp greater or equal to the specified value (HH:mm:ss.SSS format). |
| `format` | `query` | `string` | `no` | `csv` | `csv, json, ndjson, html` | The format of the data when returned to the user. |

### 输出 (200)

- 描述: Returns last NBBO quote for an option contract
- 支持格式: `csv`, `json`, `ndjson` (部分页支持 `html` 预览)

| 字段 | 类型 | 说明 |
|---|---|---|
| `symbol` | `string` | The symbol of the contract, or stock / underlying asset / option / index. |
| `expiration` | `string` | Expiration date of the contract in YYYY-MM-DD format. |
| `strike` | `number` | Strike price of the contract in dollars 180.00 |
| `right` | `string` | Indicates whether the contract is a call or put option. |
| `timestamp` | `string` | The timestamp in YYYY-MM-DDTHH:mm:ss.SSS format. |
| `sequence` | `integer` | The exchange [sequence](/Articles/Data-And-Requests/Making-Requests.html#trade-sequences). |
| `ext_condition1` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition2` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition3` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `ext_condition4` | `integer` | Additional trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html)(s). These can be ignored for options. |
| `condition` | `integer` | The trade [condition](/Articles/Errors-Exchanges-Conditions/Trade-Conditions.html). |
| `size` | `integer` | The amount of contracts / shares traded. |
| `exchange` | `integer` | The [exchange](/Articles/Errors-Exchanges-Conditions/Exchanges.html) the trade was executed. |
| `price` | `number` | The trade price. |
