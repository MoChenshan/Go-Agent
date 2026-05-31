---
name: weather
description: "Get current weather and short forecasts via Open-Meteo with a bundled script. Use when: user asks about weather, temperature, or forecasts for any location. NOT for: historical weather data, severe weather alerts, or detailed meteorological analysis. No API key needed."
metadata: { "openclaw": { "emoji": "☔", "requires": { "bins": ["python3"] } } }
---

# Weather Skill

Get current weather conditions and forecasts.

## When to Use

✅ **USE this skill when:**

- "What's the weather?"
- "Will it rain today/tomorrow?"
- "Temperature in [city]"
- "Weather forecast for the week"
- Travel planning weather checks

## When NOT to Use

❌ **DON'T use this skill when:**

- Historical weather data → use weather archives/APIs
- Climate analysis or trends → use specialized data sources
- Hyper-local microclimate data → use local sensors
- Severe weather alerts → check official NWS sources
- Aviation/marine weather → use specialized services (METAR, etc.)

## Location

Always include a city, region, or airport code in weather queries.

## Primary Command

Use the bundled script. It calls Open-Meteo directly and prints a stable
plain-text summary that the assistant can quote or summarize.

```bash
python3 {baseDir}/scripts/weather.py "London"
python3 {baseDir}/scripts/weather.py "New York" --days 5
python3 {baseDir}/scripts/weather.py "Beijing" --json
```

## Commands

Prefer the bundled script above.

### Current Weather

```bash
# Stable summary from Open-Meteo
python3 {baseDir}/scripts/weather.py "London"

# Specific city
python3 {baseDir}/scripts/weather.py "New York"
```

### Forecasts

```bash
# 3-day forecast
python3 {baseDir}/scripts/weather.py "London" --days 3

# Week forecast
python3 {baseDir}/scripts/weather.py "London" --days 7
```

### Structured Output

```bash
python3 {baseDir}/scripts/weather.py "London" --json
```

### Legacy Fallback

If you need to debug a provider issue manually, these raw endpoints may help:

```bash
curl -s "https://wttr.in/London?format=j1"
curl -s "https://geocoding-api.open-meteo.com/v1/search?name=London&count=1&language=en&format=json"
```

Avoid relying on wttr.in text formats such as `format=3` in automation.
In some environments they can be rate-limited or return placeholder text
instead of a weather summary.

## Quick Responses

**"What's the weather?"**

```bash
python3 {baseDir}/scripts/weather.py "London"
```

**"Will it rain?"**

```bash
python3 {baseDir}/scripts/weather.py "London" --days 2
```

**"Weekend forecast"**

```bash
python3 {baseDir}/scripts/weather.py "London" --days 7
```

## Notes

- No API key needed
- Uses Open-Meteo directly via the bundled script
- Works for most global cities and many airport/city names
- Prefer the script over ad hoc shell pipelines
