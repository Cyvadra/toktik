"""
Code generator: reads endpoint specs and emits a complete Go fmp package.
Run once to regenerate; output is checked in.
"""
import json, textwrap

# ── helpers ──────────────────────────────────────────────────────────────────

def go_type(py_type, val=None):
    if py_type == "str":   return "string"
    if py_type == "bool":  return "bool"
    if py_type == "int":   return "int64"
    if py_type == "float": return "float64"
    # NoneType seen in news/crypto image field
    if py_type == "NoneType": return "*string"
    return "any"

def to_go_name(s):
    """camelCase JSON key → GoExportedName"""
    parts = []
    # split on uppercase boundaries and known acronyms
    import re
    # insert _ before uppercase runs then title-case each part
    s2 = re.sub(r'([A-Z]+)([A-Z][a-z])', r'\1_\2', s)
    s2 = re.sub(r'([a-z\d])([A-Z])', r'\1_\2', s2)
    for part in s2.split('_'):
        if not part:
            continue
        up = part.upper()
        # common acronyms to keep all-caps
        if up in ('ID', 'URL', 'API', 'ESG', 'ETF', 'DCF', 'CEO', 'CIK',
                  'EPS', 'PE', 'PB', 'PS', 'EBIT', 'EBITDA', 'ADR',
                  'NYSE', 'NASDAQ', 'GDP', 'SEC', 'RSS', 'SP', 'IPO',
                  'ISIN', 'CUSIP', 'SGA', 'RD', 'CF'):
            parts.append(up)
        else:
            parts.append(part.capitalize())
    name = ''.join(parts)
    # Fix edge case: "Stock Price" has a space
    name = name.replace(' ', '')
    # Ensure starts uppercase
    if name and name[0].islower():
        name = name[0].upper() + name[1:]
    return name or "Field"

def struct_fields(sample: dict, indent="    ") -> str:
    lines = []
    for k, v in sample.items():
        go_n = to_go_name(k)
        go_t = go_type(type(v).__name__, v)
        # JSON key with space → use the key as-is
        lines.append(f'{indent}{go_n} {go_t} `json:"{k}"`')
    return "\n".join(lines)

# ── endpoint registry ─────────────────────────────────────────────────────────
# Each entry: (go_file, struct_name, json_path, http_path, params_desc, return_desc, sample_dict)

ENDPOINTS = [

# ── Search ────────────────────────────────────────────────────────────────────
("search.go", "SearchResult", "search-symbol", "/search-symbol",
 [("query","string","ticker or name to search")],
 "search-symbol returns symbols matching a query.",
 {"symbol":"AAPL","name":"Apple Inc.","currency":"USD","exchangeFullName":"NASDAQ Global Select","exchange":"NASDAQ"}),

("search.go", "CompanyScreenerResult", "company-screener", "/company-screener",
 [("marketCapMoreThan","int64","min market cap in USD (0=ignore)"),
  ("marketCapLessThan","int64","max market cap in USD (0=ignore)"),
  ("sector","string","sector filter, e.g. Technology"),
  ("industry","string","industry filter"),
  ("exchange","string","exchange short name, e.g. NASDAQ"),
  ("country","string","country code, e.g. US"),
  ("limit","int","max results")],
 "CompanyScreener filters stocks by fundamental criteria.",
 {"symbol":"NVDA","companyName":"NVIDIA Corporation","marketCap":5085821016896,"sector":"Technology",
  "industry":"Semiconductors","beta":2.335,"price":209.25,"lastAnnualDividend":0.04,
  "volume":114136225,"exchange":"NASDAQ Global Select","exchangeShortName":"NASDAQ",
  "country":"US","isEtf":False,"isFund":False,"isActivelyTrading":True}),

# ── Company ───────────────────────────────────────────────────────────────────
("company.go", "Profile", "profile", "/profile",
 [("symbol","string","ticker symbol")],
 "Profile returns the full company profile for a symbol.",
 {"symbol":"AAPL","price":270.17,"marketCap":3966403593800,"beta":1.109,"lastDividend":1.04,
  "range":"193.25-288.62","change":-0.54,"changePercentage":-0.19948,"volume":24117049,
  "averageVolume":44334911,"companyName":"Apple Inc.","currency":"USD","cik":"0000320193",
  "isin":"US0378331005","cusip":"037833100","exchangeFullName":"NASDAQ Global Select",
  "exchange":"NASDAQ","industry":"Consumer Electronics","website":"https://www.apple.com",
  "description":"Apple Inc...","ceo":"Timothy D. Cook","sector":"Technology","country":"US",
  "fullTimeEmployees":"164000","phone":"(408) 996-1010","address":"One Apple Park Way",
  "city":"Cupertino","state":"CA","zip":"95014",
  "image":"https://images.financialmodelingprep.com/symbol/AAPL.png",
  "ipoDate":"1980-12-12","defaultImage":False,"isEtf":False,"isActivelyTrading":True,
  "isAdr":False,"isFund":False}),

("company.go", "MarketCapHistory", "historical-market-capitalization", "/historical-market-capitalization",
 [("symbol","string","ticker"),("from","string","start YYYY-MM-DD"),("to","string","end YYYY-MM-DD"),("limit","int","max rows")],
 "HistoricalMarketCap returns daily market-cap history.",
 {"symbol":"AAPL","date":"2026-04-29","marketCap":3984509846860}),

("company.go", "EmployeeCount", "employee-count", "/employee-count",
 [("symbol","string","ticker")],
 "EmployeeCount returns employee count filings for a company.",
 {"symbol":"AAPL","cik":"0000320193","acceptanceTime":"2025-10-31 06:01:26",
  "periodOfReport":"2025-09-27","companyName":"Apple Inc.","formType":"10-K",
  "filingDate":"2025-10-31","employeeCount":166000,"source":"https://www.sec.gov/..."}),

("company.go", "CompanyNote", "company-notes", "/company-notes",
 [("symbol","string","ticker")],
 "CompanyNotes returns notes/bonds issued by a company.",
 {"cik":"0000320193","symbol":"AAPL","title":"0.000% Notes due 2025","exchange":"NASDAQ"}),

# ── Quote ─────────────────────────────────────────────────────────────────────
("quote.go", "Quote", "quote", "/quote",
 [("symbol","string","ticker")],
 "Quote returns the current real-time quote for a symbol.",
 {"symbol":"AAPL","name":"Apple Inc.","price":270.17,"changePercentage":-0.19948,
  "change":-0.54,"volume":24117049,"dayLow":267.04,"dayHigh":271.04,
  "yearHigh":288.62,"yearLow":193.25,"marketCap":3966403593800,
  "priceAvg50":260.6874,"priceAvg200":254.5222,"exchange":"NASDAQ",
  "open":267.55,"previousClose":270.71,"timestamp":1777492801}),

("quote.go", "QuoteShort", "quote-short", "/quote-short",
 [("symbol","string","ticker")],
 "QuoteShort returns a minimal price/change/volume snapshot.",
 {"symbol":"AAPL","price":270.17,"change":-0.54,"volume":24117049}),

# ── Financial Statements ──────────────────────────────────────────────────────
("statements.go", "IncomeStatement", "income-statement", "/income-statement",
 [("symbol","string","ticker"),("period","string","annual or quarter"),("limit","int","max rows")],
 "IncomeStatements returns income statements.",
 {"date":"2025-09-27","symbol":"AAPL","reportedCurrency":"USD","cik":"0000320193",
  "filingDate":"2025-10-31","acceptedDate":"2025-10-31 06:01:26","fiscalYear":"2025","period":"FY",
  "revenue":416161000000,"costOfRevenue":220960000000,"grossProfit":195201000000,
  "researchAndDevelopmentExpenses":34550000000,"generalAndAdministrativeExpenses":8077000000,
  "sellingAndMarketingExpenses":19524000000,"sellingGeneralAndAdministrativeExpenses":27601000000,
  "otherExpenses":0,"operatingExpenses":62151000000,"costAndExpenses":283111000000,
  "netInterestIncome":0,"interestIncome":0,"interestExpense":0,
  "depreciationAndAmortization":11698000000,"ebitda":144427000000,"ebit":132729000000,
  "nonOperatingIncomeExcludingInterest":321000000,"operatingIncome":133050000000,
  "totalOtherIncomeExpensesNet":-321000000,"incomeBeforeTax":132729000000,
  "incomeTaxExpense":20719000000,"netIncomeFromContinuingOperations":112010000000,
  "netIncomeFromDiscontinuedOperations":0,"otherAdjustmentsToNetIncome":0,
  "netIncome":112010000000,"netIncomeDeductions":0,"bottomLineNetIncome":112010000000,
  "eps":7.49,"epsDiluted":7.46,"weightedAverageShsOut":14948500000,"weightedAverageShsOutDil":15004697000}),

("statements.go", "BalanceSheet", "balance-sheet-statement", "/balance-sheet-statement",
 [("symbol","string","ticker"),("period","string","annual or quarter"),("limit","int","max rows")],
 "BalanceSheets returns balance sheet statements.",
 {"date":"2025-09-27","symbol":"AAPL","reportedCurrency":"USD","cik":"0000320193",
  "filingDate":"2025-10-31","acceptedDate":"2025-10-31 06:01:26","fiscalYear":"2025","period":"FY",
  "cashAndCashEquivalents":35934000000,"shortTermInvestments":18763000000,
  "cashAndShortTermInvestments":54697000000,"netReceivables":72957000000,
  "accountsReceivables":39777000000,"otherReceivables":33180000000,"inventory":5718000000,
  "prepaids":0,"otherCurrentAssets":14585000000,"totalCurrentAssets":147957000000,
  "propertyPlantEquipmentNet":49834000000,"goodwill":0,"intangibleAssets":0,
  "goodwillAndIntangibleAssets":0,"longTermInvestments":77723000000,"taxAssets":20777000000,
  "otherNonCurrentAssets":62950000000,"totalNonCurrentAssets":211284000000,"otherAssets":0,
  "totalAssets":359241000000,"totalPayables":82876000000,"accountPayables":69860000000,
  "otherPayables":13016000000,"accruedExpenses":8919000000,"shortTermDebt":20329000000,
  "capitalLeaseObligationsCurrent":2117000000,"taxPayables":0,"deferredRevenue":9055000000,
  "otherCurrentLiabilities":42335000000,"totalCurrentLiabilities":165631000000,
  "longTermDebt":78328000000,"capitalLeaseObligationsNonCurrent":11603000000,
  "deferredRevenueNonCurrent":0,"deferredTaxLiabilitiesNonCurrent":0,
  "otherNonCurrentLiabilities":29946000000,"totalNonCurrentLiabilities":119877000000,
  "otherLiabilities":0,"capitalLeaseObligations":13720000000,"totalLiabilities":285508000000,
  "treasuryStock":0,"preferredStock":0,"commonStock":93568000000,"retainedEarnings":-14264000000,
  "additionalPaidInCapital":0,"accumulatedOtherComprehensiveIncomeLoss":-5571000000,
  "otherTotalStockholdersEquity":0,"totalStockholdersEquity":73733000000,"totalEquity":73733000000,
  "minorityInterest":0,"totalLiabilitiesAndTotalEquity":359241000000,
  "totalInvestments":96486000000,"totalDebt":112377000000,"netDebt":76443000000}),

("statements.go", "CashFlowStatement", "cash-flow-statement", "/cash-flow-statement",
 [("symbol","string","ticker"),("period","string","annual or quarter"),("limit","int","max rows")],
 "CashFlowStatements returns cash flow statements.",
 {"date":"2025-09-27","symbol":"AAPL","reportedCurrency":"USD","cik":"0000320193",
  "filingDate":"2025-10-31","acceptedDate":"2025-10-31 06:01:26","fiscalYear":"2025","period":"FY",
  "netIncome":112010000000,"depreciationAndAmortization":11698000000,"deferredIncomeTax":0,
  "stockBasedCompensation":12863000000,"changeInWorkingCapital":-25000000000,
  "accountsReceivables":-6682000000,"inventory":1400000000,"accountsPayables":902000000,
  "otherWorkingCapital":-20620000000,"otherNonCashItems":-89000000,
  "netCashProvidedByOperatingActivities":111482000000,
  "investmentsInPropertyPlantAndEquipment":-12715000000,"acquisitionsNet":0,
  "purchasesOfInvestments":-24407000000,"salesMaturitiesOfInvestments":53797000000,
  "otherInvestingActivities":-1480000000,"netCashProvidedByInvestingActivities":15195000000,
  "netDebtIssuance":-8483000000,"longTermNetDebtIssuance":-6451000000,
  "shortTermNetDebtIssuance":-2032000000,"netStockIssuance":-90711000000,
  "netCommonStockIssuance":-90711000000,"commonStockIssuance":0,
  "commonStockRepurchased":-90711000000,"netPreferredStockIssuance":0,
  "netDividendsPaid":-15421000000,"commonDividendsPaid":-15421000000,
  "preferredDividendsPaid":0,"otherFinancingActivities":-6071000000,
  "netCashProvidedByFinancingActivities":-120686000000,"effectOfForexChangesOnCash":0,
  "netChangeInCash":5991000000,"cashAtEndOfPeriod":35934000000,
  "cashAtBeginningOfPeriod":29943000000,"operatingCashFlow":111482000000,
  "capitalExpenditure":-12715000000,"freeCashFlow":98767000000,
  "incomeTaxesPaid":43369000000,"interestPaid":0}),

# ── Statement Growth ──────────────────────────────────────────────────────────
("growth.go", "IncomeStatementGrowth", "income-statement-growth", "/income-statement-growth",
 [("symbol","string","ticker"),("period","string","annual or quarter"),("limit","int","max rows")],
 "IncomeStatementGrowth returns YoY growth rates for income statement items.",
 {"symbol":"AAPL","date":"2025-09-27","fiscalYear":"2025","period":"FY","reportedCurrency":"USD",
  "growthRevenue":0.064,"growthCostOfRevenue":0.050,"growthGrossProfit":0.080,
  "growthGrossProfitRatio":0.015,"growthResearchAndDevelopmentExpenses":0.101,
  "growthGeneralAndAdministrativeExpenses":0.083,"growthSellingAndMarketingExpenses":0.047,
  "growthOtherExpenses":0,"growthOperatingExpenses":0.081,"growthCostAndExpenses":0.057,
  "growthInterestIncome":0,"growthInterestExpense":0,"growthDepreciationAndAmortization":0.022,
  "growthEBITDA":0.070,"growthOperatingIncome":0.079,"growthIncomeBeforeTax":0.074,
  "growthIncomeTaxExpense":-0.303,"growthNetIncome":0.194,"growthEPS":0.225,
  "growthEPSDiluted":0.226,"growthWeightedAverageShsOut":-0.025,
  "growthWeightedAverageShsOutDil":-0.026,"growthEBIT":0.074,
  "growthNonOperatingIncomeExcludingInterest":2.193,"growthNetInterestIncome":0,
  "growthTotalOtherIncomeExpensesNet":-2.193,
  "growthNetIncomeFromContinuingOperations":0.194,
  "growthOtherAdjustmentsToNetIncome":0,"growthNetIncomeDeductions":0}),

("growth.go", "BalanceSheetGrowth", "balance-sheet-statement-growth", "/balance-sheet-statement-growth",
 [("symbol","string","ticker"),("period","string","annual or quarter"),("limit","int","max rows")],
 "BalanceSheetGrowth returns YoY growth rates for balance sheet items.",
 {"symbol":"AAPL","date":"2025-09-27","fiscalYear":"2025","period":"FY","reportedCurrency":"USD",
  "growthCashAndCashEquivalents":0.200,"growthShortTermInvestments":-0.467,
  "growthCashAndShortTermInvestments":-0.160,"growthNetReceivables":0.101,
  "growthInventory":-0.215,"growthOtherCurrentAssets":0.020,"growthTotalCurrentAssets":-0.032,
  "growthPropertyPlantEquipmentNet":0.090,"growthGoodwill":0,"growthIntangibleAssets":0,
  "growthGoodwillAndIntangibleAssets":0,"growthLongTermInvestments":-0.150,
  "growthTaxAssets":0.065,"growthOtherNonCurrentAssets":0.137,
  "growthTotalNonCurrentAssets":-0.003,"growthOtherAssets":0,"growthTotalAssets":-0.015,
  "growthAccountPayables":0.013,"growthShortTermDebt":-0.026,"growthTaxPayables":-1,
  "growthDeferredRevenue":0.097,"growthOtherCurrentLiabilities":-0.154,
  "growthTotalCurrentLiabilities":-0.061,"growthLongTermDebt":-0.086,
  "growthDeferredRevenueNonCurrent":0,"growthDeferredTaxLiabilitiesNonCurrent":0,
  "growthOtherNonCurrentLiabilities":-0.146,"growthTotalNonCurrentLiabilities":-0.089,
  "growthOtherLiabilities":0,"growthTotalLiabilities":-0.073,"growthPreferredStock":0,
  "growthCommonStock":0.123,"growthRetainedEarnings":0.255,
  "growthAccumulatedOtherComprehensiveIncomeLoss":0.223,
  "growthOthertotalStockholdersEquity":0,"growthTotalStockholdersEquity":0.294,
  "growthMinorityInterest":0,"growthTotalEquity":0.294,
  "growthTotalLiabilitiesAndStockholdersEquity":-0.015,"growthTotalInvestments":-0.238,
  "growthTotalDebt":-0.056,"growthNetDebt":-0.142,"growthAccountsReceivables":0.190,
  "growthOtherReceivables":0.010,"growthPrepaids":0,"growthTotalPayables":-0.132,
  "growthOtherPayables":-0.510,"growthAccruedExpenses":0,
  "growthCapitalLeaseObligationsCurrent":0.297,"growthAdditionalPaidInCapital":0,
  "growthTreasuryStock":0}),

("growth.go", "CashFlowGrowth", "cash-flow-statement-growth", "/cash-flow-statement-growth",
 [("symbol","string","ticker"),("period","string","annual or quarter"),("limit","int","max rows")],
 "CashFlowGrowth returns YoY growth rates for cash flow items.",
 {"symbol":"AAPL","date":"2025-09-27","fiscalYear":"2025","period":"FY","reportedCurrency":"USD",
  "growthNetIncome":0.194,"growthDepreciationAndAmortization":0.022,"growthDeferredIncomeTax":0,
  "growthStockBasedCompensation":0.100,"growthChangeInWorkingCapital":-7.847,
  "growthAccountsReceivables":-0.298,"growthInventory":2.338,"growthAccountsPayables":-0.850,
  "growthOtherWorkingCapital":-6.396,"growthOtherNonCashItems":0.960,
  "growthNetCashProvidedByOperatingActivites":-0.057,
  "growthInvestmentsInPropertyPlantAndEquipment":-0.345,"growthAcquisitionsNet":0,
  "growthPurchasesOfInvestments":0.498,"growthSalesMaturitiesOfInvestments":-0.137,
  "growthOtherInvestingActivites":-0.131,"growthNetCashUsedForInvestingActivites":4.177,
  "growthDebtRepayment":0.661,"growthCommonStockIssued":0,"growthCommonStockRepurchased":0.044,
  "growthDividendsPaid":-0.012,"growthOtherFinancingActivites":-0.046,
  "growthNetCashUsedProvidedByFinancingActivities":0.010,"growthEffectOfForexChangesOnCash":0,
  "growthNetChangeInCash":8.545,"growthCashAtEndOfPeriod":0.200,
  "growthCashAtBeginningOfPeriod":-0.025,"growthOperatingCashFlow":-0.057,
  "growthCapitalExpenditure":-0.345,"growthFreeCashFlow":-0.092,
  "growthNetDebtIssuance":-0.414,"growthLongTermNetDebtIssuance":0.352,
  "growthShortTermNetDebtIssuance":-1.513,"growthNetStockIssuance":0.044,
  "growthPreferredDividendsPaid":-0.012,"growthIncomeTaxesPaid":0.661,"growthInterestPaid":0}),

# ── Ratios & Metrics ──────────────────────────────────────────────────────────
("ratios.go", "Ratios", "ratios", "/ratios",
 [("symbol","string","ticker"),("period","string","annual or quarter"),("limit","int","max rows")],
 "Ratios returns valuation, profitability, and leverage ratios.",
 {"symbol":"AAPL","date":"2025-09-27","fiscalYear":"2025","period":"FY","reportedCurrency":"USD",
  "grossProfitMargin":0.469,"ebitMargin":0.318,"ebitdaMargin":0.347,"operatingProfitMargin":0.319,
  "pretaxProfitMargin":0.318,"continuousOperationsProfitMargin":0.269,"netProfitMargin":0.269,
  "bottomLineProfitMargin":0.269,"receivablesTurnover":5.704,"payablesTurnover":3.162,
  "inventoryTurnover":38.642,"fixedAssetTurnover":8.350,"assetTurnover":1.158,
  "currentRatio":0.893,"quickRatio":0.858,"solvencyRatio":0.433,"cashRatio":0.216,
  "priceToEarningsRatio":34.09,"priceToEarningsGrowthRatio":1.509,
  "forwardPriceToEarningsGrowthRatio":1.509,"priceToBookRatio":51.79,
  "priceToSalesRatio":9.176,"priceToFreeCashFlowRatio":38.664,
  "priceToOperatingCashFlowRatio":34.254,"debtToAssetsRatio":0.312,"debtToEquityRatio":1.524,
  "debtToCapitalRatio":0.603,"longTermDebtToCapitalRatio":0.515,"financialLeverageRatio":4.872,
  "workingCapitalTurnoverRatio":-20.261,"operatingCashFlowRatio":0.673,
  "operatingCashFlowSalesRatio":0.267,"freeCashFlowOperatingCashFlowRatio":0.885,
  "debtServiceCoverageRatio":6.085,"interestCoverageRatio":0,
  "shortTermOperatingCashFlowCoverageRatio":5.483,"operatingCashFlowCoverageRatio":0.992,
  "capitalExpenditureCoverageRatio":8.767,"dividendPaidAndCapexCoverageRatio":3.962,
  "dividendPayoutRatio":0.137,"dividendYield":0.004,"dividendYieldPercentage":0.403,
  "revenuePerShare":27.839,"netIncomePerShare":7.493,"interestDebtPerShare":7.517,
  "cashPerShare":3.659,"bookValuePerShare":4.932,"tangibleBookValuePerShare":4.932,
  "shareholdersEquityPerShare":4.932,"operatingCashFlowPerShare":7.457,"capexPerShare":0.850,
  "freeCashFlowPerShare":6.607,"netIncomePerEBT":0.843,"ebtPerEbit":0.997,
  "priceToFairValue":51.791,"debtToMarketCap":0.025,"effectiveTaxRate":0.156,
  "enterpriseValueMultiple":26.969,"dividendPerShare":1.031}),

("ratios.go", "RatiosTTM", "ratios-ttm", "/ratios-ttm",
 [("symbol","string","ticker")],
 "RatiosTTM returns trailing-twelve-month ratios.",
 {"symbol":"AAPL","grossProfitMarginTTM":0.473,"ebitMarginTTM":0.324,"ebitdaMarginTTM":0.351,
  "operatingProfitMarginTTM":0.323,"pretaxProfitMarginTTM":0.324,
  "continuousOperationsProfitMarginTTM":0.270,"netProfitMarginTTM":0.270,
  "bottomLineProfitMarginTTM":0.270,"receivablesTurnoverTTM":6.194,"payablesTurnoverTTM":3.250,
  "inventoryTurnoverTTM":39.057,"fixedAssetTurnoverTTM":8.684,"assetTurnoverTTM":1.148,
  "currentRatioTTM":0.973,"quickRatioTTM":0.937,"solvencyRatioTTM":0.445,"cashRatioTTM":0.279,
  "priceToEarningsRatioTTM":33.83,"priceToEarningsGrowthRatioTTM":1.325,
  "forwardPriceToEarningsGrowthRatioTTM":3.430,"priceToBookRatioTTM":45.18,
  "priceToSalesRatioTTM":9.105,"priceToFreeCashFlowRatioTTM":32.162,
  "priceToOperatingCashFlowRatioTTM":29.412,"debtToAssetsRatioTTM":0.238,
  "debtToEquityRatioTTM":1.026,"debtToCapitalRatioTTM":0.506,
  "longTermDebtToCapitalRatioTTM":0.465,"financialLeverageRatioTTM":4.300,
  "workingCapitalTurnoverRatioTTM":-39.715,"operatingCashFlowRatioTTM":0.834,
  "operatingCashFlowSalesRatioTTM":0.310,"freeCashFlowOperatingCashFlowRatioTTM":0.910,
  "debtServiceCoverageRatioTTM":9.375,"interestCoverageRatioTTM":0,
  "shortTermOperatingCashFlowCoverageRatioTTM":9.799,"operatingCashFlowCoverageRatioTTM":1.496,
  "capitalExpenditureCoverageRatioTTM":11.151,"dividendPaidAndCapexCoverageRatioTTM":4.902,
  "dividendPayoutRatioTTM":0.131,"dividendYieldTTM":0.003,"enterpriseValueTTM":4011595593800,
  "revenuePerShareTTM":29.537,"netIncomePerShareTTM":7.985,"interestDebtPerShareTTM":6.136,
  "cashPerShareTTM":4.536,"bookValuePerShareTTM":5.979,"tangibleBookValuePerShareTTM":5.979,
  "shareholdersEquityPerShareTTM":5.979,"operatingCashFlowPerShareTTM":9.185,
  "capexPerShareTTM":0.823,"freeCashFlowPerShareTTM":8.361,"netIncomePerEBTTTM":0.834,
  "ebtPerEbitTTM":1.000,"priceToFairValueTTM":45.18,"debtToMarketCapTTM":0.022,
  "effectiveTaxRateTTM":0.165,"enterpriseValueMultipleTTM":26.223,"dividendPerShareTTM":1.04}),

("ratios.go", "KeyMetrics", "key-metrics", "/key-metrics",
 [("symbol","string","ticker"),("period","string","annual or quarter"),("limit","int","max rows")],
 "KeyMetrics returns key financial metrics (market cap, EV, ROE, etc.).",
 {"symbol":"AAPL","date":"2025-09-27","fiscalYear":"2025","period":"FY","reportedCurrency":"USD",
  "marketCap":3818743810000,"enterpriseValue":3895186810000,"evToSales":9.359,
  "evToOperatingCashFlow":34.940,"evToFreeCashFlow":39.438,"evToEBITDA":26.969,
  "netDebtToEBITDA":0.529,"currentRatio":0.893,"incomeQuality":0.995,
  "grahamNumber":28.837,"grahamNetNet":-11.588,"taxBurden":0.843,"interestBurden":1,
  "workingCapital":-17674000000,"investedCapital":32160000000,"returnOnAssets":0.311,
  "operatingReturnOnAssets":0.367,"returnOnTangibleAssets":0.311,"returnOnEquity":1.519,
  "returnOnInvestedCapital":0.519,"returnOnCapitalEmployed":0.687,"earningsYield":0.029,
  "freeCashFlowYield":0.025,"capexToOperatingCashFlow":0.114,"capexToDepreciation":1.086,
  "capexToRevenue":0.030,"salesGeneralAndAdministrativeToRevenue":0.066,
  "researchAndDevelopementToRevenue":0.083,"stockBasedCompensationToRevenue":0.030,
  "intangiblesToTotalAssets":0,"averageReceivables":69600000000,"averagePayables":69410000000,
  "averageInventory":6502000000,"daysOfSalesOutstanding":63.987,"daysOfPayablesOutstanding":115.400,
  "daysOfInventoryOutstanding":9.445,"operatingCycle":73.433,"cashConversionCycle":-41.967,
  "freeCashFlowToEquity":22324000000,"freeCashFlowToFirm":105262000000,
  "tangibleAssetValue":73733000000,"netCurrentAssetValue":-137551000000}),

("ratios.go", "KeyMetricsTTM", "key-metrics-ttm", "/key-metrics-ttm",
 [("symbol","string","ticker")],
 "KeyMetricsTTM returns trailing-twelve-month key metrics.",
 {"symbol":"AAPL","marketCap":3966403593800,"enterpriseValueTTM":4011595593800,
  "evToSalesTTM":9.208,"evToOperatingCashFlowTTM":29.611,"evToFreeCashFlowTTM":32.528,
  "evToEBITDATTM":26.223,"netDebtToEBITDATTM":0.295,"currentRatioTTM":0.973,
  "incomeQualityTTM":1.150,"grahamNumberTTM":32.778,"grahamNetNetTTM":-11.426,
  "taxBurdenTTM":0.834,"interestBurdenTTM":1,"workingCapitalTTM":-4263000000,
  "investedCapitalTTM":45896000000,"returnOnAssetsTTM":0.310,"operatingReturnOnAssetsTTM":0.382,
  "returnOnTangibleAssetsTTM":0.310,"returnOnEquityTTM":1.599,
  "returnOnInvestedCapitalTTM":0.510,"returnOnCapitalEmployedTTM":0.650,
  "earningsYieldTTM":0.029,"freeCashFlowYieldTTM":0.031,"capexToOperatingCashFlowTTM":0.089,
  "capexToDepreciationTTM":1.026,"capexToRevenueTTM":0.027,
  "salesGeneralAndAdministrativeToRevenueTTM":0.064,"researchAndDevelopementToRevenueTTM":0.085,
  "stockBasedCompensationToRevenueTTM":0.030,"intangiblesToTotalAssetsTTM":0,
  "averageReceivablesTTM":71638500000,"averagePayablesTTM":70223500000,
  "averageInventoryTTM":5796500000,"daysOfSalesOutstandingTTM":58.920,
  "daysOfPayablesOutstandingTTM":112.282,"daysOfInventoryOutstandingTTM":9.345,
  "operatingCycleTTM":68.265,"cashConversionCycleTTM":-44.016,
  "freeCashFlowToEquityTTM":78132000000,"freeCashFlowToFirmTTM":104050000000,
  "tangibleAssetValueTTM":88190000000,"netCurrentAssetValueTTM":-133003000000}),

("ratios.go", "FinancialGrowth", "financial-growth", "/financial-growth",
 [("symbol","string","ticker"),("period","string","annual or quarter"),("limit","int","max rows")],
 "FinancialGrowth returns long-term revenue, income, and FCF growth rates.",
 {"symbol":"AAPL","date":"2025-09-27","fiscalYear":"2025","period":"FY","reportedCurrency":"USD",
  "revenueGrowth":0.064,"grossProfitGrowth":0.080,"ebitgrowth":0.074,
  "operatingIncomeGrowth":0.079,"netIncomeGrowth":0.194,"epsgrowth":0.225,
  "epsdilutedGrowth":0.226,"weightedAverageSharesGrowth":-0.025,
  "weightedAverageSharesDilutedGrowth":-0.026,"dividendsPerShareGrowth":0.039,
  "operatingCashFlowGrowth":-0.057,"receivablesGrowth":0.101,"inventoryGrowth":-0.215,
  "assetGrowth":-0.015,"bookValueperShareGrowth":0.328,"debtGrowth":-0.056,
  "rdexpenseGrowth":0.101,"sgaexpensesGrowth":0.057,"freeCashFlowGrowth":-0.092,
  "tenYRevenueGrowthPerShare":1.741,"fiveYRevenueGrowthPerShare":0.759,
  "threeYRevenueGrowthPerShare":0.144,"tenYOperatingCFGrowthPerShare":1.111,
  "fiveYOperatingCFGrowthPerShare":0.604,"threeYOperatingCFGrowthPerShare":-0.009,
  "tenYNetIncomeGrowthPerShare":2.229,"fiveYNetIncomeGrowthPerShare":1.264,
  "threeYNetIncomeGrowthPerShare":0.217,"tenYShareholdersEquityGrowthPerShare":-0.048,
  "fiveYShareholdersEquityGrowthPerShare":0.309,"threeYShareholdersEquityGrowthPerShare":0.578,
  "tenYDividendperShareGrowthPerShare":1.053,"fiveYDividendperShareGrowthPerShare":0.271,
  "threeYDividendperShareGrowthPerShare":0.127,"ebitdaGrowth":0.070,
  "growthCapitalExpenditure":-0.345,"tenYBottomLineNetIncomeGrowthPerShare":2.229,
  "fiveYBottomLineNetIncomeGrowthPerShare":1.264,"threeYBottomLineNetIncomeGrowthPerShare":0.217}),

# ── Valuation ─────────────────────────────────────────────────────────────────
("valuation.go", "DCF", "discounted-cash-flow", "/discounted-cash-flow",
 [("symbol","string","ticker")],
 "DCF returns the current DCF valuation estimate.",
 {"symbol":"AAPL","date":"2026-04-30","dcf":155.92,"Stock Price":270.17}),

# ── Price / Charts ────────────────────────────────────────────────────────────
("price.go", "EODPrice", "historical-price-eod/full", "/historical-price-eod/full",
 [("symbol","string","ticker"),("from","string","start YYYY-MM-DD"),("to","string","end YYYY-MM-DD")],
 "HistoricalPrices returns daily EOD OHLCV price bars.",
 {"symbol":"AAPL","date":"2025-04-01","open":219.81,"high":223.68,"low":218.9,
  "close":223.19,"volume":36412740,"change":3.38,"changePercent":1.54,"vwap":221.395}),

("price.go", "AdjustedPrice", "historical-price-eod/dividend-adjusted",
 "/historical-price-eod/dividend-adjusted",
 [("symbol","string","ticker"),("from","string","start YYYY-MM-DD"),("to","string","end YYYY-MM-DD")],
 "AdjustedPrices returns dividend-adjusted historical prices.",
 {"symbol":"AAPL","date":"2025-04-01","adjOpen":218.85,"adjHigh":222.71,
  "adjLow":217.95,"adjClose":222.22,"volume":36412740}),

# ── Analyst ───────────────────────────────────────────────────────────────────
("analyst.go", "PriceTargetSummary", "price-target-summary", "/price-target-summary",
 [("symbol","string","ticker")],
 "PriceTargetSummary returns aggregated analyst price target statistics.",
 {"symbol":"AAPL","lastMonthCount":2,"lastMonthAvgPriceTarget":293.5,
  "lastQuarterCount":11,"lastQuarterAvgPriceTarget":309.73,"lastYearCount":51,
  "lastYearAvgPriceTarget":286.86,"allTimeCount":234,"allTimeAvgPriceTarget":221.75,
  "publishers":"[...]"}),

("analyst.go", "PriceTargetConsensus", "price-target-consensus", "/price-target-consensus",
 [("symbol","string","ticker")],
 "PriceTargetConsensus returns consensus price target (high/low/median/consensus).",
 {"symbol":"AAPL","targetHigh":350,"targetLow":239,"targetConsensus":313.95,"targetMedian":325}),

# ── News ──────────────────────────────────────────────────────────────────────
("news.go", "NewsArticle", "news/stock", "/news/stock",
 [("symbol","string","ticker"),("limit","int","max articles")],
 "StockNews returns recent news articles for a symbol.",
 {"symbol":"AAPL","publishedDate":"2026-04-29 19:21:41","publisher":"CNET",
  "title":"Apple Reportedly Plans Major Camera AI Overhaul",
  "image":"https://images.financialmodelingprep.com/news/...","site":"cnet.com",
  "text":"Another update may help iPhone users...","url":"https://www.cnet.com/..."}),

("news.go", "ForexNewsArticle", "news/forex", "/news/forex",
 [("symbol","string","forex pair, e.g. EURUSD"),("limit","int","max articles")],
 "ForexNews returns recent forex market news.",
 {"symbol":"EURUSD","publishedDate":"2026-04-30 02:58:59","publisher":"FX Street",
  "title":"EUR/USD dips towards 1.1650","image":"https://...","site":"fxstreet.com",
  "text":"EUR/USD dips...","url":"https://www.fxstreet.com/..."}),

("news.go", "CryptoNewsArticle", "news/crypto", "/news/crypto",
 [("limit","int","max articles")],
 "CryptoNews returns recent crypto market news.",
 {"symbol":"BTCUSD","publishedDate":"2026-04-30 03:17:58","publisher":"Blockonomi",
  "title":"Fed Maintains Interest Rates","image":None,"site":"blockonomi.com",
  "text":"The Federal Reserve opted to maintain...","url":"https://blockonomi.com/..."}),

("news.go", "PressRelease", "news/press-releases", "/news/press-releases",
 [("symbol","string","ticker"),("limit","int","max articles")],
 "PressReleases returns press releases for a symbol.",
 {"symbol":"AAPL","publishedDate":"2026-04-29 08:00:00","publisher":"GlobeNewsWire",
  "title":"GD Culture Group Provides Business Progress on AI Initiative",
  "image":"https://...","site":"globenewswire.com",
  "text":"NEW YORK, April 29, 2026...","url":"https://www.globenewswire.com/..."}),

# ── Market / Index ────────────────────────────────────────────────────────────
("market.go", "SP500Change", "historical-sp500-constituent", "/historical-sp500-constituent",
 [("limit","int","max rows")],
 "HistoricalSP500Constituents returns S&P 500 addition/removal history.",
 {"dateAdded":"April 09, 2026","addedSecurity":"Casey's General Stores, Inc.",
  "removedTicker":"HOLX","removedSecurity":"Hologic, Inc.","date":"2026-04-09",
  "symbol":"CASY","reason":"Blackstone Inc. and TPG Inc. acquired Hologic."}),

("market.go", "SectorPE", "historical-sector-pe", "/historical-sector-pe",
 [("sector","string","e.g. Technology"),("exchange","string","e.g. NASDAQ"),
  ("from","string","start YYYY-MM-DD"),("to","string","end YYYY-MM-DD"),("limit","int","max rows")],
 "HistoricalSectorPE returns historical P/E ratios by sector.",
 {"date":"2024-03-01","sector":"Technology","exchange":"NASDAQ","pe":0.774}),

# ── Economics ─────────────────────────────────────────────────────────────────
("economics.go", "TreasuryRate", "treasury-rates", "/treasury-rates",
 [("from","string","start YYYY-MM-DD"),("to","string","end YYYY-MM-DD")],
 "TreasuryRates returns US Treasury yield curve rates.",
 {"date":"2025-04-01","month1":4.38,"month2":4.35,"month3":4.32,"month6":4.23,
  "year1":4.01,"year2":3.87,"year3":3.85,"year5":3.91,"year7":4.03,
  "year10":4.17,"year20":4.56,"year30":4.52}),

("economics.go", "EconomicIndicator", "economic-indicators", "/economic-indicators",
 [("name","string","indicator name e.g. GDP, CPI, unemployment"),("limit","int","max rows")],
 "EconomicIndicators returns historical values for a macro indicator.",
 {"name":"GDP","date":"2025-10-01","value":31422.526}),

# ── ESG ───────────────────────────────────────────────────────────────────────
("esg.go", "ESGDisclosure", "esg-disclosures", "/esg-disclosures",
 [("symbol","string","ticker"),("limit","int","max rows")],
 "ESGDisclosures returns ESG scores from SEC filings.",
 {"date":"2025-12-27","acceptedDate":"2026-01-29","symbol":"AAPL","cik":"0000320193",
  "companyName":"Apple Inc.","formType":"8-K","environmentalScore":52.42,
  "socialScore":45.16,"governanceScore":60.79,"ESGScore":52.79,
  "url":"https://www.sec.gov/..."}),

("esg.go", "ESGRating", "esg-ratings", "/esg-ratings",
 [("symbol","string","ticker")],
 "ESGRatings returns ESG risk rating history for a company.",
 {"symbol":"AAPL","cik":"0000320193","companyName":"Apple Inc.",
  "industry":"CONSUMER ELECTRONICS","fiscalYear":2001,
  "ESGRiskRating":"B","industryRank":"4 out of 6"}),
]

# ── code generation ───────────────────────────────────────────────────────────

FILE_HEADER = '''// Code generated by generate.py — DO NOT EDIT.
// Re-run: python3 generate.py
package fmp

import (
\t"context"
\t"fmt"
\t"net/url"
)
'''

FILE_HEADER_NO_FMT = '''// Code generated by generate.py — DO NOT EDIT.
// Re-run: python3 generate.py
package fmp

import (
\t"context"
\t"net/url"
)
'''

def build_struct(struct_name, sample):
    lines = [f"// {struct_name} is a row returned by the FMP {struct_name} endpoint.",
             f"type {struct_name} struct {{"]
    for k, v in sample.items():
        go_n = to_go_name(k)
        py_t = type(v).__name__
        # special: int from JSON that holds large numbers should be float64 if it has decimals
        if py_t == "int" and isinstance(v, float):
            py_t = "float"
        go_t = go_type(py_t, v)
        # clean key for JSON tag
        json_key = k
        lines.append(f"\t{go_n} {go_t} `json:\"{json_key}\"`")
    lines.append("}")
    return "\n".join(lines)

def build_method(struct_name, endpoint_key, http_path, params, doc):
    """Generate a method on *Client that returns []StructName."""
    # determine method name from endpoint_key
    parts = endpoint_key.replace("/","_").replace("-","_").split("_")
    method = "".join(p.capitalize() for p in parts if p)
    # fix common method names
    name_map = {
        "SearchSymbol": "SearchSymbol",
        "CompanyScreener": "Screener",
        "Profile": "Profile",
        "HistoricalMarketCapitalization": "HistoricalMarketCap",
        "EmployeeCount": "EmployeeCount",
        "CompanyNotes": "CompanyNotes",
        "Quote": "Quote",
        "QuoteShort": "QuoteShort",
        "BatchQuote": "BatchQuote",
        "BatchQuoteShort": "BatchQuoteShort",
        "IncomeStatement": "IncomeStatements",
        "BalanceSheetStatement": "BalanceSheets",
        "CashFlowStatement": "CashFlowStatements",
        "IncomeStatementGrowth": "IncomeGrowth",
        "BalanceSheetStatementGrowth": "BalanceSheetGrowth",
        "CashFlowStatementGrowth": "CashFlowGrowth",
        "Ratios": "Ratios",
        "RatiosTtm": "RatiosTTM",
        "KeyMetrics": "KeyMetrics",
        "KeyMetricsTtm": "KeyMetricsTTM",
        "FinancialGrowth": "FinancialGrowth",
        "DiscountedCashFlow": "DCF",
        "HistoricalPriceEodFull": "HistoricalPrices",
        "HistoricalPriceEodDividendAdjusted": "AdjustedPrices",
        "PriceTargetSummary": "PriceTargetSummary",
        "PriceTargetConsensus": "PriceTargetConsensus",
        "NewsStock": "StockNews",
        "NewsForex": "ForexNews",
        "NewsCrypto": "CryptoNews",
        "NewsPressReleases": "PressReleases",
        "HistoricalSp500Constituent": "HistoricalSP500",
        "HistoricalSectorPe": "HistoricalSectorPE",
        "TreasuryRates": "TreasuryRates",
        "EconomicIndicators": "EconomicIndicators",
        "EsgDisclosures": "ESGDisclosures",
        "EsgRatings": "ESGRatings",
    }
    method = name_map.get(method, method)

    # return type: single item for *TTM and single-record endpoints
    single = struct_name in ("RatiosTTM", "KeyMetricsTTM", "Profile",
                              "PriceTargetSummary","PriceTargetConsensus","DCF")

    # build param struct
    go_params = []
    url_sets = []
    sig_params = ["ctx context.Context"]

    for pname, ptype, pdesc in params:
        go_t = {"string":"string","int":"int","int64":"int64"}[ptype]
        sig_params.append(f"{pname} {go_t}")
        if ptype == "string":
            url_sets.append(
                f'\tif {pname} != "" {{\n\t\tparams.Set("{pname}", {pname})\n\t}}'
            )
        else:
            url_sets.append(
                f'\tif {pname} > 0 {{\n\t\tparams.Set("{pname}", fmt.Sprintf("%d", {pname}))\n\t}}'
            )

    # first string param is usually required (symbol / query)
    required_params = [p for p in params if p[1]=="string" and p[0] in ("symbol","query","name","cik")]

    lines = [f"// {method} {doc}"]
    if single:
        lines.append(f"func (c *Client) {method}({', '.join(sig_params)}) (*{struct_name}, error) {{")
    else:
        lines.append(f"func (c *Client) {method}({', '.join(sig_params)}) ([]{struct_name}, error) {{")

    lines.append("\tparams := url.Values{}")
    lines.extend(url_sets)
    lines.append(f'\tvar out []{struct_name}')
    lines.append(f'\tif err := c.get(ctx, "{http_path}", params, &out); err != nil {{')
    lines.append(f'\t\treturn nil, err')
    lines.append(f'\t}}')
    if single:
        lines.append(f'\tif len(out) == 0 {{')
        lines.append(f'\t\treturn nil, fmt.Errorf("fmp: no result for {method}")')
        lines.append(f'\t}}')
        lines.append(f'\treturn &out[0], nil')
    else:
        lines.append(f'\treturn out, nil')
    lines.append("}")
    return "\n".join(lines)

# group by file
from collections import defaultdict
by_file = defaultdict(list)
for entry in ENDPOINTS:
    go_file, struct_name, endpoint_key, http_path, params, doc, sample = entry
    by_file[go_file].append((struct_name, endpoint_key, http_path, params, doc, sample))

import os
out_dir = "/home/claude/fmpfull/fmp"
os.makedirs(out_dir, exist_ok=True)

for go_file, entries in by_file.items():
    structs_seen = set()
    struct_blocks = []
    method_blocks = []
    needs_fmt = False

    for struct_name, endpoint_key, http_path, params, doc, sample in entries:
        if struct_name not in structs_seen:
            structs_seen.add(struct_name)
            struct_blocks.append(build_struct(struct_name, sample))

        method_code = build_method(struct_name, endpoint_key, http_path, params, doc)
        if "fmt.Sprintf" in method_code or 'fmt.Errorf' in method_code:
            needs_fmt = True
        method_blocks.append(method_code)

    header = FILE_HEADER if needs_fmt else FILE_HEADER_NO_FMT
    content = header + "\n\n" + "\n\n".join(struct_blocks) + "\n\n" + "\n\n".join(method_blocks) + "\n"
    path = os.path.join(out_dir, go_file)
    with open(path, "w") as f:
        f.write(content)
    print(f"wrote {path} ({len(content)} chars)")

print("\nDone.")
