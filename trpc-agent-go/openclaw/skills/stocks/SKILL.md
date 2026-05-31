---
name: stocks
description: "Search and quote listed securities across Mainland China, Hong Kong, and U.S. markets via Tencent Finance public endpoints, with listed-fund discovery fallback. Use when: the user asks for a stock, ETF, LOF, REIT, ADR, ticker, current price, change, market cap, PE, 52-week range, or wants a quick comparison across several securities. NOT for: investment advice, portfolio analysis, long historical backtests, or guaranteed exchange-grade market data. No API key needed."
metadata: { "openclaw": { "emoji": "📈", "requires": { "bins": ["python3"] } } }
---

# Stocks Skill

Use the bundled script for stable listed-security lookup and quote
snapshots.

Markets

- Mainland China A-shares
- Mainland China listed ETFs, LOFs, and REITs
- Hong Kong stocks
- U.S. stocks and ADRs

When to use

- Find the ticker for a company name
- Find the ticker for an ETF, LOF, or REIT
- Get the latest quote for one security
- Compare several securities side by side
- Check day range, 52-week range, PE, and market cap

When not to use

- Investment advice or trading recommendations
- Tick-level or exchange-grade market data
- Long historical backtests
- Portfolio analytics or broker account operations

Primary commands

```bash
python3 {baseDir}/scripts/stocks.py search "Tencent"
python3 {baseDir}/scripts/stocks.py search "纳指科技ETF"
python3 {baseDir}/scripts/stocks.py quote AAPL 00700 600519
python3 {baseDir}/scripts/stocks.py quote "纳指嘉实 ETF" "景顺长城纳指科技ETF"
python3 {baseDir}/scripts/stocks.py quote "Tesla" --json
```

Search first when ambiguous

If the user gives a company name or a short ambiguous query, search first:

```bash
python3 {baseDir}/scripts/stocks.py search "Tencent" --limit 5
python3 {baseDir}/scripts/stocks.py search "Tesla"
python3 {baseDir}/scripts/stocks.py search "贵州茅台"
python3 {baseDir}/scripts/stocks.py search "嘉实纳指 ETF"
```

Direct quotes

The `quote` command accepts:

- Company names such as `Tencent` or `Tesla`
- Listed fund names such as `纳指ETF嘉实` or `纳指科技ETF景顺`
- Plain tickers such as `AAPL`, `00700`, `600519`
- Canonical symbols such as `usAAPL`, `hk00700`, `sh600519`

Examples

```bash
python3 {baseDir}/scripts/stocks.py quote AAPL
python3 {baseDir}/scripts/stocks.py quote 00700
python3 {baseDir}/scripts/stocks.py quote 600519
python3 {baseDir}/scripts/stocks.py quote 159501 159509
python3 {baseDir}/scripts/stocks.py quote "Tencent" "Apple" "Tesla"
```

Structured output

```bash
python3 {baseDir}/scripts/stocks.py search "Tencent" --json
python3 {baseDir}/scripts/stocks.py quote AAPL TSLA --json
```

Notes

- No API key is required.
- The script uses Tencent Finance public quote/search endpoints and a
  listed-fund discovery fallback for harder ETF-style queries.
- The data is suitable for quick lookup and comparison, not for trading
  execution.
- If a query is ambiguous, the script resolves the best listed-security
  match and shows close alternatives.
