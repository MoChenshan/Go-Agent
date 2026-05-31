#!/usr/bin/env python3

"""Search and quote listed securities via public market data endpoints."""

from __future__ import annotations

import argparse
import json
import re
import sys
import unicodedata
import urllib.error
import urllib.parse
import urllib.request
from dataclasses import asdict, dataclass
from typing import Iterable


REQUEST_TIMEOUT_SECONDS = 12
SEARCH_RESULT_LIMIT = 5
SEARCH_VARIANT_LIMIT = 12
QUERY_SEGMENT_MIN = 2
QUERY_SEGMENT_MAX = 4

USER_AGENT_HEADER = "trpc-claw-stocks-skill/1.0"
REFERER_HEADER = "https://finance.qq.com/"

SEARCH_ENDPOINT = "https://smartbox.gtimg.cn/s3/"
QUOTE_ENDPOINT = "https://qt.gtimg.cn/q="
FUND_SEARCH_ENDPOINT = (
    "https://fundsuggest.eastmoney.com/FundSearch/api/"
    "FundSearchAPI.ashx"
)

HEADER_USER_AGENT = "User-Agent"
HEADER_REFERER = "Referer"

FIELD_SEPARATOR = "~"
RECORD_SEPARATOR = "^"
QUOTE_PREFIX = "v_"
QUOTE_SUFFIX = "\";"
HINT_PREFIX = "v_hint=\""
HINT_SUFFIX = "\""
STATEMENT_SUFFIX = ";"

SUPPORTED_MARKETS = frozenset({"sh", "sz", "bj", "hk", "us"})
STOCK_KIND_PREFIX = "GP"
REIT_KIND = "FJ"
UNKNOWN_KIND = "UNKNOWN"

LISTED_SECURITY_KIND_TOKENS = frozenset({"ETF", "LOF", "REIT"})
SECURITY_SEARCH_TOKENS = ("ETF", "LOF", "REITS", "REIT")
ASCII_SECURITY_TOKEN_PATTERN = re.compile(r"etf|lof|reits?", re.IGNORECASE)
CHINESE_CHAR_PATTERN = re.compile(r"[\u4e00-\u9fff]")
DIGIT_CODE_PATTERN = re.compile(r"^\d{6}$")

MARKET_MAINLAND = "mainland_cn"
MARKET_HONG_KONG = "hong_kong"
MARKET_US = "united_states"

MARKET_LABELS = {
    "sh": MARKET_MAINLAND,
    "sz": MARKET_MAINLAND,
    "bj": MARKET_MAINLAND,
    "hk": MARKET_HONG_KONG,
    "us": MARKET_US,
}

MARKET_DISPLAY_NAMES = {
    MARKET_MAINLAND: "Mainland China",
    MARKET_HONG_KONG: "Hong Kong",
    MARKET_US: "United States",
}

MARKET_CURRENCIES = {
    MARKET_MAINLAND: "CNY",
    MARKET_HONG_KONG: "HKD",
    MARKET_US: "USD",
}

MAINLAND_MARKET_KEYS = frozenset({"sh", "sz", "bj"})

SEARCH_FIELD_MARKET = 0
SEARCH_FIELD_CODE = 1
SEARCH_FIELD_NAME = 2
SEARCH_FIELD_ALIAS = 3
SEARCH_FIELD_KIND = 4

QUOTE_FIELD_NAME = 1
QUOTE_FIELD_CODE = 2
QUOTE_FIELD_PRICE = 3
QUOTE_FIELD_PREV_CLOSE = 4
QUOTE_FIELD_OPEN = 5
QUOTE_FIELD_VOLUME = 6
QUOTE_FIELD_TIME = 30
QUOTE_FIELD_CHANGE = 31
QUOTE_FIELD_CHANGE_PERCENT = 32
QUOTE_FIELD_HIGH = 33
QUOTE_FIELD_LOW = 34
QUOTE_FIELD_PE = 39
QUOTE_FIELD_MARKET_CAP = 44
QUOTE_FIELD_WEEK_52_HIGH = 48
QUOTE_FIELD_WEEK_52_LOW = 49
QUOTE_FIELD_MAINLAND_WEEK_52_HIGH = 47
QUOTE_FIELD_MAINLAND_WEEK_52_LOW = 48

FLOAT_FIELD_NAMES = (
    "price",
    "prev_close",
    "open_price",
    "change",
    "change_percent",
    "high",
    "low",
    "pe_ratio",
    "market_cap_base",
    "week_52_high",
    "week_52_low",
    "volume_shares",
)

CANONICAL_SYMBOL_PATTERN = re.compile(
    r"^(?:(sh|sz|bj)(\d{6})|(hk)(\d{5})|(us)([A-Za-z0-9.\-]+))$",
)

NON_ALNUM_PATTERN = re.compile(r"[^0-9A-Za-z\u4e00-\u9fff]+")

MAINLAND_CODE_MARKET_HINTS = {
    "0": ("sz", "sh", "bj"),
    "1": ("sz", "sh", "bj"),
    "2": ("sz", "sh", "bj"),
    "3": ("sz", "sh", "bj"),
    "4": ("bj", "sh", "sz"),
    "5": ("sh", "sz", "bj"),
    "6": ("sh", "sz", "bj"),
    "8": ("bj", "sh", "sz"),
    "9": ("sh", "sz", "bj"),
}


@dataclass(frozen=True)
class Candidate:
    market: str
    raw_code: str
    name: str
    alias: str
    kind: str
    symbol: str
    market_group: str


@dataclass(frozen=True)
class Resolution:
    query: str
    selected: Candidate
    alternatives: tuple[Candidate, ...]
    exact: bool


@dataclass(frozen=True)
class Quote:
    symbol: str
    market: str
    market_group: str
    code: str
    name: str
    currency: str
    price: float | None
    prev_close: float | None
    open_price: float | None
    change: float | None
    change_percent: float | None
    high: float | None
    low: float | None
    volume_shares: float | None
    pe_ratio: float | None
    market_cap_base: float | None
    week_52_high: float | None
    week_52_low: float | None
    quote_time: str


@dataclass(frozen=True)
class FundSuggestion:
    raw_code: str
    name: str
    alias: str
    company: str


def request_headers() -> dict[str, str]:
    return {
        HEADER_USER_AGENT: USER_AGENT_HEADER,
        HEADER_REFERER: REFERER_HEADER,
    }


def fetch_text(url: str, encoding: str | None = None) -> str:
    request = urllib.request.Request(url, headers=request_headers())
    with urllib.request.urlopen(
        request,
        timeout=REQUEST_TIMEOUT_SECONDS,
    ) as response:
        payload = response.read()
    if encoding:
        return payload.decode(encoding, errors="replace")
    for candidate in ("utf-8", "gbk"):
        try:
            return payload.decode(candidate)
        except UnicodeDecodeError:
            continue
    return payload.decode("utf-8", errors="replace")


def market_group_for_prefix(market: str) -> str:
    group = MARKET_LABELS.get(market)
    if not group:
        raise ValueError(f"unsupported market prefix: {market}")
    return group


def canonical_symbol(market: str, raw_code: str) -> str:
    code = raw_code.strip()
    if market == "us":
        code = code.split(".", 1)[0].upper()
    return f"{market}{code}"


def try_direct_symbol(query: str) -> Candidate | None:
    match = CANONICAL_SYMBOL_PATTERN.match(query.strip())
    if not match:
        return None
    market = ""
    raw_code = ""
    if match.group(1) and match.group(2):
        market = match.group(1).lower()
        raw_code = match.group(2)
    elif match.group(3) and match.group(4):
        market = match.group(3).lower()
        raw_code = match.group(4)
    elif match.group(5) and match.group(6):
        market = match.group(5).lower()
        raw_code = match.group(6)
    else:
        return None
    return Candidate(
        market=market,
        raw_code=raw_code,
        name="",
        alias="",
        kind=UNKNOWN_KIND,
        symbol=canonical_symbol(market, raw_code),
        market_group=market_group_for_prefix(market),
    )


def normalize_for_match(value: str) -> str:
    compact = NON_ALNUM_PATTERN.sub("", value).lower()
    return compact


def normalize_search_query(query: str) -> str:
    compact = unicodedata.normalize("NFKC", query).strip()
    compact = ASCII_SECURITY_TOKEN_PATTERN.sub(
        lambda match: match.group(0).upper(),
        compact,
    )
    compact = re.sub(r"\s+", "", compact)
    return compact


def search_query_token(query: str) -> str | None:
    normalized = normalize_search_query(query)
    for token in SECURITY_SEARCH_TOKENS:
        if token in normalized:
            return token
    return None


def should_try_fund_variants(query: str) -> bool:
    normalized = normalize_search_query(query)
    return bool(
        CHINESE_CHAR_PATTERN.search(normalized)
        and search_query_token(normalized),
    )


def add_query_variant(
    variants: list[str],
    seen: set[str],
    value: str,
) -> None:
    trimmed = value.strip()
    if not trimmed or trimmed in seen:
        return
    seen.add(trimmed)
    variants.append(trimmed)


def edge_segment_lengths(value: str) -> range:
    upper = min(QUERY_SEGMENT_MAX, len(value) - 1)
    if upper < QUERY_SEGMENT_MIN:
        return range(0)
    return range(QUERY_SEGMENT_MIN, upper + 1)


def search_query_variants(query: str) -> tuple[str, ...]:
    normalized = normalize_search_query(query)
    variants: list[str] = []
    seen: set[str] = set()
    add_query_variant(variants, seen, query.strip())
    add_query_variant(variants, seen, normalized)

    token = search_query_token(normalized)
    if not token or not CHINESE_CHAR_PATTERN.search(normalized):
        return tuple(variants)

    prefix, _, suffix = normalized.partition(token)
    if suffix:
        return tuple(variants[:SEARCH_VARIANT_LIMIT])

    lengths = tuple(edge_segment_lengths(prefix))
    for length in reversed(lengths):
        trailing = prefix[-length:]
        trailing_rest = prefix[:-length]
        if len(trailing_rest) >= QUERY_SEGMENT_MIN:
            add_query_variant(variants, seen, trailing_rest + token)
            add_query_variant(
                variants,
                seen,
                trailing_rest + token + trailing,
            )

        leading = prefix[:length]
        leading_rest = prefix[length:]
        if len(leading_rest) >= QUERY_SEGMENT_MIN:
            add_query_variant(variants, seen, leading_rest + token)
            add_query_variant(
                variants,
                seen,
                leading_rest + token + leading,
            )

    return tuple(variants[:SEARCH_VARIANT_LIMIT])


def positive_int(value: str) -> int:
    parsed = int(value)
    if parsed < 1:
        raise argparse.ArgumentTypeError("value must be >= 1")
    return parsed


def decode_escaped_field(value: str) -> str:
    if "\\u" not in value and "\\x" not in value:
        return value
    return value.encode("utf-8").decode("unicode_escape")


def split_hint_payload(payload: str) -> str:
    trimmed = payload.strip()
    if trimmed.endswith(STATEMENT_SUFFIX):
        trimmed = trimmed[: -len(STATEMENT_SUFFIX)].rstrip()
    if not trimmed.startswith(HINT_PREFIX):
        raise ValueError("unexpected search payload format")
    if not trimmed.endswith(HINT_SUFFIX):
        raise ValueError("unexpected search payload format")
    return trimmed[len(HINT_PREFIX) : -len(HINT_SUFFIX)]


def is_supported_search_kind(kind: str) -> bool:
    normalized = kind.strip().upper()
    if normalized.startswith(STOCK_KIND_PREFIX):
        return True
    if normalized == REIT_KIND:
        return True
    return any(
        token in normalized
        for token in LISTED_SECURITY_KIND_TOKENS
    )


def parse_search_payload(payload: str) -> list[Candidate]:
    content = split_hint_payload(payload)
    if not content:
        return []
    candidates: list[Candidate] = []
    for record in content.split(RECORD_SEPARATOR):
        fields = record.split(FIELD_SEPARATOR)
        if len(fields) <= SEARCH_FIELD_KIND:
            continue
        market = fields[SEARCH_FIELD_MARKET].strip().lower()
        kind = fields[SEARCH_FIELD_KIND].strip().upper()
        if market not in SUPPORTED_MARKETS:
            continue
        if not is_supported_search_kind(kind):
            continue
        raw_code = fields[SEARCH_FIELD_CODE].strip()
        name = decode_escaped_field(fields[SEARCH_FIELD_NAME].strip())
        alias = decode_escaped_field(fields[SEARCH_FIELD_ALIAS].strip())
        candidates.append(
            Candidate(
                market=market,
                raw_code=raw_code,
                name=name,
                alias=alias,
                kind=kind,
                symbol=canonical_symbol(market, raw_code),
                market_group=market_group_for_prefix(market),
            )
        )
    return candidates


def search_tencent_candidates(query: str) -> list[Candidate]:
    params = urllib.parse.urlencode(
        {
            "v": 2,
            "q": query,
            "t": "all",
        }
    )
    payload = fetch_text(f"{SEARCH_ENDPOINT}?{params}")
    return parse_search_payload(payload)


def parse_fund_search_payload(payload: str) -> list[FundSuggestion]:
    try:
        parsed = json.loads(payload)
    except json.JSONDecodeError:
        return []
    if not isinstance(parsed, dict):
        return []
    raw_items = parsed.get("Datas")
    if not isinstance(raw_items, list):
        return []

    suggestions: list[FundSuggestion] = []
    for item in raw_items:
        if not isinstance(item, dict):
            continue
        if item.get("CATEGORY") != 700:
            continue
        raw_code = str(item.get("CODE", "")).strip()
        if not DIGIT_CODE_PATTERN.fullmatch(raw_code):
            continue
        name = str(item.get("NAME", "")).strip()
        if not name:
            continue
        alias = str(item.get("JP", "")).strip()
        company = ""
        fund_info = item.get("FundBaseInfo")
        if isinstance(fund_info, dict):
            company = str(fund_info.get("JJGS", "")).strip()
        suggestions.append(
            FundSuggestion(
                raw_code=raw_code,
                name=name,
                alias=alias,
                company=company,
            )
        )
    return suggestions


def mainland_probe_symbols(raw_code: str) -> tuple[str, ...]:
    order = MAINLAND_CODE_MARKET_HINTS.get(
        raw_code[:1],
        ("sh", "sz", "bj"),
    )
    return tuple(canonical_symbol(market, raw_code) for market in order)


def build_candidate_from_suggestion(
    suggestion: FundSuggestion,
    quotes: dict[str, Quote],
) -> Candidate | None:
    for symbol in mainland_probe_symbols(suggestion.raw_code):
        quote = quotes.get(symbol)
        if quote is None or quote.code != suggestion.raw_code or not quote.name:
            continue
        alias_parts = [suggestion.alias]
        if suggestion.company:
            alias_parts.append(suggestion.company)
        return Candidate(
            market=symbol[:2],
            raw_code=suggestion.raw_code,
            name=quote.name,
            alias=" ".join(part for part in alias_parts if part),
            kind="FUND",
            symbol=symbol,
            market_group=market_group_for_prefix(symbol[:2]),
        )
    return None


def search_fund_suggestions(query: str) -> list[Candidate]:
    suggestions_by_code: dict[str, FundSuggestion] = {}
    for variant in search_query_variants(query):
        params = urllib.parse.urlencode({"m": 1, "key": variant})
        payload = fetch_text(f"{FUND_SEARCH_ENDPOINT}?{params}")
        for suggestion in parse_fund_search_payload(payload):
            suggestions_by_code.setdefault(
                suggestion.raw_code,
                suggestion,
            )

    if not suggestions_by_code:
        return []

    probe_symbols: list[str] = []
    for suggestion in suggestions_by_code.values():
        probe_symbols.extend(mainland_probe_symbols(suggestion.raw_code))
    quotes = fetch_quotes(probe_symbols)

    candidates: list[Candidate] = []
    for suggestion in suggestions_by_code.values():
        candidate = build_candidate_from_suggestion(suggestion, quotes)
        if candidate is not None:
            candidates.append(candidate)
    return candidates


def search_candidates(query: str) -> list[Candidate]:
    candidates_by_symbol: dict[str, Candidate] = {}
    for variant in search_query_variants(query):
        for candidate in search_tencent_candidates(variant):
            candidates_by_symbol.setdefault(
                candidate.symbol,
                candidate,
            )

    if candidates_by_symbol or not should_try_fund_variants(query):
        return sorted(
            candidates_by_symbol.values(),
            key=lambda item: candidate_match_score(query, item),
            reverse=True,
        )

    for candidate in search_fund_suggestions(query):
        candidates_by_symbol.setdefault(candidate.symbol, candidate)
    return sorted(
        candidates_by_symbol.values(),
        key=lambda item: candidate_match_score(query, item),
        reverse=True,
    )


def candidate_match_score_for_query(
    query: str,
    candidate: Candidate,
) -> tuple[int, ...]:
    query_norm = normalize_for_match(query)
    raw_norm = normalize_for_match(candidate.raw_code)
    raw_base_norm = normalize_for_match(
        candidate.raw_code.split(".", 1)[0],
    )
    symbol_norm = normalize_for_match(candidate.symbol)
    name_norm = normalize_for_match(candidate.name)
    alias_norm = normalize_for_match(candidate.alias)

    return (
        1 if query_norm == symbol_norm else 0,
        1 if query_norm == raw_norm else 0,
        1 if query_norm == raw_base_norm else 0,
        1 if query_norm == name_norm else 0,
        1 if query_norm == alias_norm else 0,
        1 if query_norm and query_norm in name_norm else 0,
        1 if query_norm and query_norm in alias_norm else 0,
        1 if raw_base_norm.startswith(query_norm) and query_norm else 0,
        1 if name_norm.startswith(query_norm) and query_norm else 0,
        1 if alias_norm.startswith(query_norm) and query_norm else 0,
    )


def candidate_match_score(query: str, candidate: Candidate) -> tuple[int, ...]:
    best_score = candidate_match_score_for_query(query, candidate)
    for variant in search_query_variants(query):
        score = candidate_match_score_for_query(variant, candidate)
        if score > best_score:
            best_score = score
    return best_score


def candidate_exact_match(query: str, candidate: Candidate) -> bool:
    for variant in search_query_variants(query):
        if any(candidate_match_score_for_query(variant, candidate)[:5]):
            return True
    return False


def resolve_query(query: str) -> Resolution:
    direct = try_direct_symbol(query)
    if direct:
        return Resolution(
            query=query,
            selected=direct,
            alternatives=(),
            exact=True,
        )

    candidates = search_candidates(query)
    if not candidates:
        raise ValueError(f"no listed security match found for {query!r}")

    ranked = sorted(
        candidates,
        key=lambda item: candidate_match_score(query, item),
        reverse=True,
    )
    selected = ranked[0]
    exact = candidate_exact_match(query, selected)
    alternatives = tuple(ranked[1:SEARCH_RESULT_LIMIT])
    return Resolution(
        query=query,
        selected=selected,
        alternatives=alternatives,
        exact=exact,
    )


def parse_float(value: str) -> float | None:
    cleaned = value.strip()
    if not cleaned:
        return None
    try:
        return float(cleaned)
    except ValueError:
        return None


def volume_in_shares(
    market: str,
    raw_volume: float | None,
) -> float | None:
    if raw_volume is None:
        return None
    if market in MAINLAND_MARKET_KEYS:
        return raw_volume * 100
    return raw_volume


def parse_quote_fields(symbol: str, fields: list[str]) -> Quote:
    market = symbol[:2]
    market_group = market_group_for_prefix(market)
    currency = MARKET_CURRENCIES[market_group]
    raw_volume = parse_float(fields[QUOTE_FIELD_VOLUME])
    week_52_high_index = QUOTE_FIELD_WEEK_52_HIGH
    week_52_low_index = QUOTE_FIELD_WEEK_52_LOW
    if market in MAINLAND_MARKET_KEYS:
        week_52_high_index = QUOTE_FIELD_MAINLAND_WEEK_52_HIGH
        week_52_low_index = QUOTE_FIELD_MAINLAND_WEEK_52_LOW

    return Quote(
        symbol=symbol,
        market=market,
        market_group=market_group,
        code=fields[QUOTE_FIELD_CODE].strip(),
        name=fields[QUOTE_FIELD_NAME].strip(),
        currency=currency,
        price=parse_float(fields[QUOTE_FIELD_PRICE]),
        prev_close=parse_float(fields[QUOTE_FIELD_PREV_CLOSE]),
        open_price=parse_float(fields[QUOTE_FIELD_OPEN]),
        change=parse_float(fields[QUOTE_FIELD_CHANGE]),
        change_percent=parse_float(fields[QUOTE_FIELD_CHANGE_PERCENT]),
        high=parse_float(fields[QUOTE_FIELD_HIGH]),
        low=parse_float(fields[QUOTE_FIELD_LOW]),
        volume_shares=volume_in_shares(market, raw_volume),
        pe_ratio=parse_float(fields[QUOTE_FIELD_PE]),
        market_cap_base=parse_float(fields[QUOTE_FIELD_MARKET_CAP]),
        week_52_high=parse_float(fields[week_52_high_index]),
        week_52_low=parse_float(fields[week_52_low_index]),
        quote_time=fields[QUOTE_FIELD_TIME].strip(),
    )


def parse_quote_payload(payload: str) -> dict[str, Quote]:
    quotes: dict[str, Quote] = {}
    for line in payload.splitlines():
        stripped = line.strip()
        if not stripped.startswith(QUOTE_PREFIX) or "=" not in stripped:
            continue
        head, tail = stripped.split("=", 1)
        symbol = head[len(QUOTE_PREFIX) :].strip()
        body = tail.strip()
        if body.endswith(QUOTE_SUFFIX):
            body = body[: -len(QUOTE_SUFFIX)]
        body = body.strip('"')
        fields = body.split(FIELD_SEPARATOR)
        if len(fields) <= QUOTE_FIELD_WEEK_52_LOW:
            continue
        quotes[symbol] = parse_quote_fields(symbol, fields)
    return quotes


def fetch_quotes(symbols: Iterable[str]) -> dict[str, Quote]:
    ordered = []
    seen: set[str] = set()
    for symbol in symbols:
        if symbol in seen:
            continue
        seen.add(symbol)
        ordered.append(symbol)
    payload = fetch_text(
        f"{QUOTE_ENDPOINT}{','.join(ordered)}",
        encoding="gbk",
    )
    return parse_quote_payload(payload)


def format_decimal(value: float | None, digits: int = 2) -> str:
    if value is None:
        return "n/a"
    text = f"{value:.{digits}f}"
    return text.rstrip("0").rstrip(".")


def format_signed(value: float | None, digits: int = 2) -> str:
    if value is None:
        return "n/a"
    text = f"{value:+.{digits}f}"
    if "." in text:
        text = text.rstrip("0").rstrip(".")
    return text


def format_percent(value: float | None) -> str:
    if value is None:
        return "n/a"
    return f"{format_signed(value)}%"


def format_human_number(value: float) -> str:
    thresholds = (
        (1_000_000_000_000, "T"),
        (1_000_000_000, "B"),
        (1_000_000, "M"),
        (1_000, "K"),
    )
    absolute = abs(value)
    for threshold, suffix in thresholds:
        if absolute >= threshold:
            return f"{value / threshold:.2f}".rstrip("0").rstrip(".") + suffix
    return format_decimal(value)


def format_shares(value: float | None) -> str:
    if value is None:
        return "n/a"
    return f"{format_human_number(value)} shares"


def format_market_cap(
    value: float | None,
    currency: str,
) -> str:
    if value is None:
        return "n/a"
    base_value = value * 100_000_000
    return f"{format_human_number(base_value)} {currency}"


def candidate_summary(candidate: Candidate) -> str:
    market_name = MARKET_DISPLAY_NAMES[candidate.market_group]
    return (
        f"{candidate.symbol} | {candidate.name} | "
        f"{market_name} | type={candidate.kind} | raw={candidate.raw_code}"
    )


def quote_to_dict(quote: Quote) -> dict[str, object]:
    payload = asdict(quote)
    payload["market_name"] = MARKET_DISPLAY_NAMES[quote.market_group]
    payload["quote_time"] = format_quote_time(quote.quote_time)
    payload["market_cap"] = format_market_cap(
        quote.market_cap_base,
        quote.currency,
    )
    for name in FLOAT_FIELD_NAMES:
        value = payload[name]
        if isinstance(value, float):
            payload[name] = round(value, 6)
    return payload


def render_search_text(
    query: str,
    candidates: list[Candidate],
    limit: int,
) -> str:
    lines = [f"Search: {query}"]
    for index, candidate in enumerate(candidates[:limit], start=1):
        lines.append(f"{index}. {candidate_summary(candidate)}")
    if len(lines) == 1:
        lines.append("No supported listed security result found.")
    return "\n".join(lines)


def render_quote_text(
    resolution: Resolution,
    quote: Quote,
) -> str:
    lines = [
        (
            f"{quote.name} ({quote.symbol}) | "
            f"{MARKET_DISPLAY_NAMES[quote.market_group]}"
        ),
    ]
    if resolution.query.strip() != quote.symbol:
        lines.append(
            f"Resolved from query: {resolution.query} -> {quote.symbol}",
        )
    if resolution.alternatives and not resolution.exact:
        alternatives = ", ".join(
            candidate.symbol for candidate in resolution.alternatives[:3]
        )
        lines.append(f"Close alternatives: {alternatives}")
    lines.append(
        (
            f"Price: {format_decimal(quote.price)} {quote.currency} "
            f"({format_signed(quote.change)}, "
            f"{format_percent(quote.change_percent)})"
        ),
    )
    lines.append(
        (
            "Open / High / Low / Prev close: "
            f"{format_decimal(quote.open_price)} / "
            f"{format_decimal(quote.high)} / "
            f"{format_decimal(quote.low)} / "
            f"{format_decimal(quote.prev_close)} {quote.currency}"
        ),
    )
    lines.append(f"Volume: {format_shares(quote.volume_shares)}")
    lines.append(
        f"Market cap: {format_market_cap(quote.market_cap_base, quote.currency)}",
    )
    lines.append(f"PE (TTM): {format_decimal(quote.pe_ratio)}")
    lines.append(
        (
            "52-week range: "
            f"{format_decimal(quote.week_52_low)} to "
            f"{format_decimal(quote.week_52_high)} {quote.currency}"
        ),
    )
    if quote.quote_time:
        lines.append(
            f"Quote time: {format_quote_time(quote.quote_time)}",
        )
    return "\n".join(lines)


def format_quote_time(value: str) -> str:
    compact = value.strip()
    if re.fullmatch(r"\d{14}", compact):
        return (
            f"{compact[0:4]}-{compact[4:6]}-{compact[6:8]} "
            f"{compact[8:10]}:{compact[10:12]}:{compact[12:14]}"
        )
    return compact


def search_command(args: argparse.Namespace) -> int:
    try:
        candidates = search_candidates(args.query)
    except (urllib.error.HTTPError, urllib.error.URLError) as err:
        print(f"security search failed: {err}", file=sys.stderr)
        return 1
    if args.json:
        payload = {
            "source": "tencent-finance",
            "query": args.query,
            "items": [asdict(item) for item in candidates[: args.limit]],
        }
        print(json.dumps(payload, indent=2, ensure_ascii=False))
        return 0
    print(render_search_text(args.query, candidates, args.limit))
    return 0 if candidates else 1


def quote_command(args: argparse.Namespace) -> int:
    resolutions: list[Resolution] = []
    errors: list[str] = []
    for query in args.queries:
        try:
            resolutions.append(resolve_query(query))
        except (
            ValueError,
            urllib.error.HTTPError,
            urllib.error.URLError,
        ) as err:
            errors.append(f"{query}: {err}")
    if not resolutions:
        for error in errors:
            print(f"security lookup failed: {error}", file=sys.stderr)
        return 1

    quotes = fetch_quotes(
        resolution.selected.symbol for resolution in resolutions
    )
    items = []
    text_blocks = []
    for resolution in resolutions:
        quote = quotes.get(resolution.selected.symbol)
        if quote is None:
            errors.append(
                (
                    f"{resolution.query}: quote data missing for "
                    f"{resolution.selected.symbol}"
                ),
            )
            continue
        items.append(
            {
                "query": resolution.query,
                "resolved": asdict(resolution.selected),
                "exact_match": resolution.exact,
                "alternatives": [
                    asdict(item) for item in resolution.alternatives
                ],
                "quote": quote_to_dict(quote),
            }
        )
        text_blocks.append(render_quote_text(resolution, quote))

    if args.json:
        payload = {
            "source": "tencent-finance",
            "items": items,
            "errors": errors,
        }
        print(json.dumps(payload, indent=2, ensure_ascii=False))
    else:
        if text_blocks:
            print("\n\n".join(text_blocks))
        for error in errors:
            print(f"warning: {error}", file=sys.stderr)
    return 0 if items else 1


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        description="Search and quote listed securities across markets.",
    )
    subparsers = parser.add_subparsers(dest="command", required=True)

    search_parser = subparsers.add_parser(
        "search",
        help="Search a company, ETF, REIT, or ticker.",
    )
    search_parser.add_argument(
        "query",
        help="Company name, ETF, REIT, or ticker.",
    )
    search_parser.add_argument(
        "--limit",
        type=positive_int,
        default=SEARCH_RESULT_LIMIT,
        help="Maximum number of search results to print.",
    )
    search_parser.add_argument(
        "--json",
        action="store_true",
        help="Print JSON instead of plain text.",
    )
    search_parser.set_defaults(func=search_command)

    quote_parser = subparsers.add_parser(
        "quote",
        help="Fetch quote snapshots for one or more securities.",
    )
    quote_parser.add_argument(
        "queries",
        nargs="+",
        help="Tickers, names, or canonical symbols.",
    )
    quote_parser.add_argument(
        "--json",
        action="store_true",
        help="Print JSON instead of plain text.",
    )
    quote_parser.set_defaults(func=quote_command)

    return parser


def main() -> int:
    parser = build_parser()
    args = parser.parse_args()
    try:
        return args.func(args)
    except (urllib.error.HTTPError, urllib.error.URLError) as err:
        print(f"security lookup failed: {err}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
