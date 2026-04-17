"""Stubbed weather data for April 2026.

The agent's `get_weather` tool reads from this table, so the demo is
deterministic and needs no external weather API. Values are fictional.
"""

from __future__ import annotations

from dataclasses import dataclass


@dataclass(frozen=True)
class Forecast:
    city: str
    date: str
    condition: str
    high_c: int
    low_c: int
    precipitation_mm: float


_FORECASTS: dict[tuple[str, str], Forecast] = {
    ("london", "2026-04-17"): Forecast("London", "2026-04-17", "Overcast with light drizzle", 14, 8, 2.1),
    ("london", "2026-04-18"): Forecast("London", "2026-04-18", "Partly cloudy", 16, 9, 0.0),
    ("london", "2026-04-19"): Forecast("London", "2026-04-19", "Sunny intervals", 18, 10, 0.0),
    ("paris", "2026-04-17"): Forecast("Paris", "2026-04-17", "Sunny", 21, 11, 0.0),
    ("paris", "2026-04-18"): Forecast("Paris", "2026-04-18", "Sunny", 22, 12, 0.0),
    ("paris", "2026-04-19"): Forecast("Paris", "2026-04-19", "Thunderstorms in the evening", 20, 13, 8.4),
    ("new york", "2026-04-17"): Forecast("New York", "2026-04-17", "Rain", 12, 7, 11.5),
    ("new york", "2026-04-18"): Forecast("New York", "2026-04-18", "Clearing, breezy", 15, 6, 0.2),
    ("new york", "2026-04-19"): Forecast("New York", "2026-04-19", "Sunny and cool", 17, 8, 0.0),
    ("tokyo", "2026-04-17"): Forecast("Tokyo", "2026-04-17", "Clear, cherry blossom season winding down", 19, 12, 0.0),
    ("tokyo", "2026-04-18"): Forecast("Tokyo", "2026-04-18", "Clear", 21, 13, 0.0),
    ("tokyo", "2026-04-19"): Forecast("Tokyo", "2026-04-19", "Light rain", 18, 14, 3.6),
    ("san francisco", "2026-04-17"): Forecast("San Francisco", "2026-04-17", "Morning fog, sunny afternoon", 17, 11, 0.0),
    ("san francisco", "2026-04-18"): Forecast("San Francisco", "2026-04-18", "Sunny", 19, 12, 0.0),
    ("san francisco", "2026-04-19"): Forecast("San Francisco", "2026-04-19", "Windy, partly cloudy", 16, 10, 0.0),
}


def lookup_forecast(city: str, date: str) -> Forecast | None:
    return _FORECASTS.get((city.strip().lower(), date.strip()))


def known_cities() -> list[str]:
    return sorted({f.city for f in _FORECASTS.values()})
