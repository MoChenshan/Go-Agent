#!/usr/bin/env python3

import argparse
import json
import sys
import urllib.error
import urllib.parse
import urllib.request


REQUEST_TIMEOUT_SECONDS = 12
MAX_FORECAST_DAYS = 7

GEOCODING_ENDPOINT = (
    "https://geocoding-api.open-meteo.com/v1/search"
)
FORECAST_ENDPOINT = "https://api.open-meteo.com/v1/forecast"

CURRENT_FIELDS = ",".join(
    [
        "temperature_2m",
        "apparent_temperature",
        "relative_humidity_2m",
        "precipitation",
        "weather_code",
        "wind_speed_10m",
    ]
)

DAILY_FIELDS = ",".join(
    [
        "weather_code",
        "temperature_2m_max",
        "temperature_2m_min",
        "precipitation_probability_max",
    ]
)

WEATHER_CODES = {
    0: "Clear sky",
    1: "Mainly clear",
    2: "Partly cloudy",
    3: "Overcast",
    45: "Fog",
    48: "Depositing rime fog",
    51: "Light drizzle",
    53: "Moderate drizzle",
    55: "Dense drizzle",
    56: "Light freezing drizzle",
    57: "Dense freezing drizzle",
    61: "Slight rain",
    63: "Moderate rain",
    65: "Heavy rain",
    66: "Light freezing rain",
    67: "Heavy freezing rain",
    71: "Slight snow",
    73: "Moderate snow",
    75: "Heavy snow",
    77: "Snow grains",
    80: "Slight rain showers",
    81: "Moderate rain showers",
    82: "Violent rain showers",
    85: "Slight snow showers",
    86: "Heavy snow showers",
    95: "Thunderstorm",
    96: "Thunderstorm with light hail",
    99: "Thunderstorm with heavy hail",
}


def fetch_json(url: str) -> dict:
    request = urllib.request.Request(
        url,
        headers={"User-Agent": "openclaw-weather-skill/1.0"},
    )
    with urllib.request.urlopen(
        request,
        timeout=REQUEST_TIMEOUT_SECONDS,
    ) as response:
        return json.load(response)


def geocode(location: str) -> dict:
    query = urllib.parse.urlencode(
        {
            "name": location,
            "count": 1,
            "language": "en",
            "format": "json",
        }
    )
    payload = fetch_json(f"{GEOCODING_ENDPOINT}?{query}")
    results = payload.get("results") or []
    if not results:
        raise ValueError(f"location not found: {location}")
    return results[0]


def fetch_forecast(place: dict, days: int) -> dict:
    query = urllib.parse.urlencode(
        {
            "latitude": place["latitude"],
            "longitude": place["longitude"],
            "current": CURRENT_FIELDS,
            "daily": DAILY_FIELDS,
            "timezone": "auto",
            "forecast_days": days,
        }
    )
    return fetch_json(f"{FORECAST_ENDPOINT}?{query}")


def weather_text(code: int) -> str:
    return WEATHER_CODES.get(code, f"Unknown ({code})")


def place_label(place: dict) -> str:
    parts = [place.get("name", "")]
    admin1 = place.get("admin1", "")
    country = place.get("country", "")
    for value in [admin1, country]:
        if value and value not in parts:
            parts.append(value)
    return ", ".join(part for part in parts if part)


def day_label(index: int, date_text: str) -> str:
    if index == 0:
        return "Today"
    if index == 1:
        return "Tomorrow"
    return date_text


def build_text(place: dict, forecast: dict) -> str:
    current = forecast["current"]
    daily = forecast["daily"]

    lines = [place_label(place)]
    lines.append(
        "Current: "
        f"{weather_text(current['weather_code'])}, "
        f"{round(current['temperature_2m'])}C "
        f"(feels {round(current['apparent_temperature'])}C), "
        f"humidity {round(current['relative_humidity_2m'])}%, "
        f"wind {round(current['wind_speed_10m'])} km/h, "
        f"precipitation {current['precipitation']} mm"
    )

    times = daily.get("time", [])
    codes = daily.get("weather_code", [])
    maxes = daily.get("temperature_2m_max", [])
    mins = daily.get("temperature_2m_min", [])
    precip = daily.get("precipitation_probability_max", [])

    for index, date_text in enumerate(times):
        lines.append(
            f"{day_label(index, date_text)}: "
            f"{weather_text(codes[index])}, "
            f"{round(mins[index])}C to {round(maxes[index])}C, "
            f"max precipitation chance {round(precip[index])}%"
        )
    return "\n".join(lines)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Fetch current weather and a short forecast."
    )
    parser.add_argument(
        "location",
        nargs="+",
        help="City, region, or airport code.",
    )
    parser.add_argument(
        "--days",
        type=int,
        default=3,
        help="Forecast days to include (1-7).",
    )
    parser.add_argument(
        "--json",
        action="store_true",
        help="Print structured JSON instead of plain text.",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    location = " ".join(args.location).strip()
    days = min(max(args.days, 1), MAX_FORECAST_DAYS)

    try:
        place = geocode(location)
        forecast = fetch_forecast(place, days)
    except (
        ValueError,
        urllib.error.HTTPError,
        urllib.error.URLError,
        TimeoutError,
    ) as err:
        print(f"weather lookup failed: {err}", file=sys.stderr)
        return 1

    if args.json:
        payload = {
            "source": "open-meteo",
            "place": place,
            "forecast": forecast,
        }
        print(json.dumps(payload, indent=2, ensure_ascii=True))
        return 0

    print(build_text(place, forecast))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
