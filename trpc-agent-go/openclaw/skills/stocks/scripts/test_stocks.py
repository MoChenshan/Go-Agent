#!/usr/bin/env python3
"""Tests for stocks skill helpers."""

import importlib.util
from pathlib import Path
import sys
from unittest import TestCase, main

MODULE_PATH = Path(__file__).with_name("stocks.py")
SPEC = importlib.util.spec_from_file_location("stocks", MODULE_PATH)
assert SPEC and SPEC.loader
MODULE = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = MODULE
SPEC.loader.exec_module(MODULE)


SEARCH_PAYLOAD = (
    'v_hint="sh~000847~腾讯济安~txja~ZS^'
    'hk~00700~\\u817e\\u8baf\\u63a7\\u80a1~txkg~GP^'
    'us~tcehy.ps~Tencent Holdings Limited~txkgadr~GP^'
    'sz~159501~\\u7eb3\\u6307ETF\\u5609\\u5b9e~nzetfjs~QDII-ETF^'
    'sh~508006~\\u5bcc\\u56fd\\u9996\\u521b\\u6c34\\u52a1REIT~'
    'fgscswreit~FJ^'
    'jj~007005~中金新医药股票C~zjxyygpc~KJ";'
)

ETF_SEARCH_PAYLOAD = (
    'v_hint="sz~159509~\\u7eb3\\u6307\\u79d1\\u6280ETF\\u666f\\u987a~'
    'nzkjetfjs~QDII-ETF";'
)

FUND_SEARCH_PAYLOAD = """
{
  "ErrCode": 0,
  "ErrMsg": "fromes",
  "Datas": [
    {
      "CODE": "159509",
      "NAME": "纳指科技ETF景顺",
      "JP": "NZKJETFJS",
      "CATEGORY": 700,
      "FundBaseInfo": {
        "JJGS": "景顺长城基金"
      }
    },
    {
      "CODE": "F0T001",
      "NAME": "华金证券致远1号",
      "JP": "HJZQZY1H",
      "CATEGORY": 750
    }
  ]
}
"""

QUOTE_PAYLOAD = """v_sh600519="1~贵州茅台~600519~1465.02~1440.02~1460.00~33836~18827~15009~1465.01~2~1465.00~23~1464.99~5~1464.92~3~1464.91~1~1465.02~0~1465.11~3~1465.13~1~1465.18~3~1465.20~3~~20260408161419~25.00~1.74~1469.08~1452.13~1465.02/33836/4947937007~33836~494794~0.27~20.38~~1469.08~1452.13~1.18~18346.01~18346.01~8.08~1584.02~1296.02~1.08~24~1462.32~21.29~21.28~~~0.48~494793.7007~0.0000~0~ ~GP-A~6.38~1.04~3.53~35.02~30.58~1593.44~1322.01~4.10~4.50~2.74~1252270215~1252270215~54.55~5.03~1252270215~~~-1.90~0.03~~CNY~0~___D__F__N~1465.50~-22~";
v_hk00700="100~腾讯控股~00700~508.000~489.200~504.500~32856156.0~0~0~508.000~0~0~0~0~0~0~0~0~0~508.000~0~0~0~0~0~0~0~0~0~32856156.0~2026/04/08 16:08:20~18.800~3.84~510.000~501.000~508.000~32856156.0~16614494503.611~0~18.62~~0~0~1.84~46358.0360~46358.0360~TENCENT~0.89~683.000~414.500~1.45~8.45~0~0~0~0~0~18.62~3.65~0.36~100~-15.19~2.96~GP~19.48~11.27~0.00~-2.12~-19.68~9125597636.00~9125597636.00~18.62~4.531~505.674~1.60~HKD~1~50";
v_usAAPL="200~苹果~AAPL.OQ~258.30~253.50~258.45~6880612~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~0~~2026-04-08 09:59:20~4.80~1.89~258.76~256.53~USD~6880612~1774917666~0.05~32.70~~34.62~~0.88~37897.95391~37921.38462~Apple Inc.~7.90~288.36~171.15~0~43.00~0.40~37921.38462~-4.90~1.78~GP~152.02~32.56~2.65~-0.97~-0.32~14681140000~14672068878~2.11~22.74~1.04~257.96~~~";
v_sz159509="51~纳指科技ETF景顺~159509~2.090~2.123~2.100~44075~21553~22522~2.089~421~2.088~600~2.087~1500~2.086~442~2.085~300~2.090~330~2.091~201~2.092~1634~2.093~291~2.094~300~~20260409094418~-0.033~-1.55~2.098~2.080~2.090/44075/9205092~44075~921~0.21~~~2.098~2.080~0.85~93.82~93.82~0.00~2.414~1.691~5.60~-374~2.090~~~~~~920.5092~0.0000~0~ ~ETF~-11.33~2.32~~~~2.252~1.822~3.02~0.10~-2.88~4488985235~4488985235~-6.62~8.52~4488985235~0.83~1.7433~19.89~0.27~1.8192~CNY~0~~2.091~-9482~";"""


class TestStocks(TestCase):
    def test_try_direct_symbol_parses_mainland(self):
        candidate = MODULE.try_direct_symbol("sh600519")
        self.assertIsNotNone(candidate)
        assert candidate is not None
        self.assertEqual(candidate.symbol, "sh600519")
        self.assertEqual(candidate.market_group, MODULE.MARKET_MAINLAND)

    def test_try_direct_symbol_parses_us_symbol(self):
        candidate = MODULE.try_direct_symbol("usAAPL")
        self.assertIsNotNone(candidate)
        assert candidate is not None
        self.assertEqual(candidate.symbol, "usAAPL")
        self.assertEqual(candidate.market_group, MODULE.MARKET_US)

    def test_parse_search_payload_keeps_supported_security_kinds(self):
        candidates = MODULE.parse_search_payload(SEARCH_PAYLOAD)
        self.assertEqual(
            [item.symbol for item in candidates],
            ["hk00700", "usTCEHY", "sz159501", "sh508006"],
        )

    def test_resolve_query_prefers_exact_name_and_filters_indexes(self):
        original = MODULE.resolve_query.__globals__["search_candidates"]
        MODULE.resolve_query.__globals__["search_candidates"] = (
            lambda query: MODULE.parse_search_payload(SEARCH_PAYLOAD)
        )
        try:
            resolution = MODULE.resolve_query("腾讯控股")
        finally:
            MODULE.resolve_query.__globals__["search_candidates"] = original
        self.assertEqual(resolution.selected.symbol, "hk00700")
        self.assertTrue(resolution.exact)

    def test_search_query_variants_reorder_etf_queries(self):
        variants = MODULE.search_query_variants("景顺长城 纳指科技 ETF")
        self.assertIn("景顺长城纳指科技ETF", variants)
        self.assertIn("纳指科技ETF", variants)
        self.assertIn("纳指科技ETF景顺长城", variants)

        trailing_variants = MODULE.search_query_variants(
            "纳指科技景顺长城 ETF",
        )
        self.assertIn("纳指科技ETF", trailing_variants)
        self.assertIn("纳指科技ETF景顺长城", trailing_variants)

    def test_resolve_query_uses_query_variants_for_etf_names(self):
        original = MODULE.resolve_query.__globals__["search_tencent_candidates"]
        MODULE.resolve_query.__globals__["search_tencent_candidates"] = (
            lambda query: MODULE.parse_search_payload(ETF_SEARCH_PAYLOAD)
            if query == "纳指科技ETF"
            else []
        )
        try:
            resolution = MODULE.resolve_query("景顺长城纳指科技ETF")
        finally:
            MODULE.resolve_query.__globals__["search_tencent_candidates"] = (
                original
            )
        self.assertEqual(resolution.selected.symbol, "sz159509")

    def test_search_candidates_ranks_etf_results_by_query(self):
        original = MODULE.search_candidates.__globals__["search_tencent_candidates"]
        payload = (
            'v_hint="sz~159941~纳指ETF广发~nzetfgf~QDII-ETF^'
            'sz~159501~纳指ETF嘉实~nzetfjs~QDII-ETF";'
        )
        MODULE.search_candidates.__globals__["search_tencent_candidates"] = (
            lambda query: MODULE.parse_search_payload(payload)
            if query == "纳指ETF"
            else []
        )
        try:
            candidates = MODULE.search_candidates("嘉实纳指ETF")
        finally:
            MODULE.search_candidates.__globals__["search_tencent_candidates"] = (
                original
            )
        self.assertEqual([item.symbol for item in candidates], ["sz159501", "sz159941"])

    def test_search_fund_suggestions_builds_mainland_candidates(self):
        fetch_text = MODULE.search_fund_suggestions.__globals__["fetch_text"]
        fetch_quotes = MODULE.search_fund_suggestions.__globals__["fetch_quotes"]
        MODULE.search_fund_suggestions.__globals__["fetch_text"] = (
            lambda url: FUND_SEARCH_PAYLOAD
        )
        MODULE.search_fund_suggestions.__globals__["fetch_quotes"] = (
            lambda symbols: MODULE.parse_quote_payload(QUOTE_PAYLOAD)
        )
        try:
            candidates = MODULE.search_fund_suggestions("景顺长城纳指科技ETF")
        finally:
            MODULE.search_fund_suggestions.__globals__["fetch_text"] = fetch_text
            MODULE.search_fund_suggestions.__globals__["fetch_quotes"] = (
                fetch_quotes
            )
        self.assertEqual([item.symbol for item in candidates], ["sz159509"])
        self.assertIn("景顺长城基金", candidates[0].alias)

    def test_parse_quote_payload_parses_main_markets(self):
        quotes = MODULE.parse_quote_payload(QUOTE_PAYLOAD)
        mainland = quotes["sh600519"]
        hong_kong = quotes["hk00700"]
        us_quote = quotes["usAAPL"]
        etf_quote = quotes["sz159509"]

        self.assertEqual(mainland.market_group, MODULE.MARKET_MAINLAND)
        self.assertEqual(hong_kong.market_group, MODULE.MARKET_HONG_KONG)
        self.assertEqual(us_quote.market_group, MODULE.MARKET_US)
        self.assertEqual(mainland.name, "贵州茅台")
        self.assertEqual(hong_kong.name, "腾讯控股")
        self.assertEqual(us_quote.name, "苹果")
        self.assertEqual(etf_quote.name, "纳指科技ETF景顺")
        self.assertAlmostEqual(mainland.volume_shares, 3383600)
        self.assertAlmostEqual(hong_kong.volume_shares, 32856156)
        self.assertAlmostEqual(us_quote.volume_shares, 6880612)
        self.assertAlmostEqual(etf_quote.volume_shares, 4407500)
        self.assertAlmostEqual(mainland.market_cap_base, 18346.01)
        self.assertAlmostEqual(hong_kong.market_cap_base, 46358.036)
        self.assertAlmostEqual(us_quote.market_cap_base, 37897.95391)
        self.assertAlmostEqual(etf_quote.market_cap_base, 93.82)
        self.assertAlmostEqual(mainland.week_52_high, 1584.02)
        self.assertAlmostEqual(mainland.week_52_low, 1296.02)
        self.assertEqual(
            MODULE.format_quote_time(mainland.quote_time),
            "2026-04-08 16:14:19",
        )

    def test_format_shares_uses_human_units(self):
        self.assertEqual(MODULE.format_shares(3383600), "3.38M shares")

    def test_format_market_cap_uses_currency(self):
        self.assertEqual(
            MODULE.format_market_cap(37897.95391, "USD"),
            "3.79T USD",
        )

    def test_market_display_names_cover_all_used_groups(self):
        used = {
            MODULE.MARKET_MAINLAND,
            MODULE.MARKET_HONG_KONG,
            MODULE.MARKET_US,
        }
        self.assertEqual(
            used,
            set(MODULE.MARKET_DISPLAY_NAMES.keys()) & used,
        )


if __name__ == "__main__":
    main()
