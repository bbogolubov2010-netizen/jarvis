// Package weather provides local-language weather forecasts from Open-Meteo.
package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultForecastURL = "https://api.open-meteo.com/v1/forecast"
	defaultGeoURL      = "https://geocoding-api.open-meteo.com/v1/search"
	defaultIPURL       = "https://ipwho.is/"

	maxWeatherResponseBytes = 2 << 20
)

// Location is a resolved place with coordinates.
type Location struct {
	Name      string
	Country   string
	Admin1    string
	Latitude  float64
	Longitude float64
	Timezone  string
}

// Current describes current conditions.
type Current struct {
	Time        string
	Temperature float64
	Apparent    float64
	Humidity    float64
	Wind        float64
	Code        int
}

// Daily describes one forecast day.
type Daily struct {
	Date       string
	Code       int
	Min        float64
	Max        float64
	PrecipProb float64
}

// Forecast is a normalized Open-Meteo response.
type Forecast struct {
	Location Location
	Period   string
	Current  Current
	Daily    []Daily
}

// Client talks to Open-Meteo. The URL fields are exported so tests can point
// the client at an httptest server.
type Client struct {
	HTTP        *http.Client
	ForecastURL string
	GeoURL      string
	IPURL       string
}

// NewClient returns a client with sane network timeouts.
func NewClient() *Client {
	return &Client{
		HTTP:        &http.Client{Timeout: 15 * time.Second},
		ForecastURL: defaultForecastURL,
		GeoURL:      defaultGeoURL,
		IPURL:       defaultIPURL,
	}
}

type geoResponse struct {
	Results []struct {
		Name      string  `json:"name"`
		Country   string  `json:"country"`
		Admin1    string  `json:"admin1"`
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Timezone  string  `json:"timezone"`
	} `json:"results"`
}

type ipResponse struct {
	Success   bool    `json:"success"`
	City      string  `json:"city"`
	Country   string  `json:"country"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Timezone  struct {
		ID string `json:"id"`
	} `json:"timezone"`
}

type forecastResponse struct {
	Current struct {
		Time        string  `json:"time"`
		Temperature float64 `json:"temperature_2m"`
		Apparent    float64 `json:"apparent_temperature"`
		Humidity    float64 `json:"relative_humidity_2m"`
		Wind        float64 `json:"wind_speed_10m"`
		WeatherCode int     `json:"weather_code"`
	} `json:"current"`
	Daily struct {
		Time       []string  `json:"time"`
		Code       []int     `json:"weather_code"`
		Max        []float64 `json:"temperature_2m_max"`
		Min        []float64 `json:"temperature_2m_min"`
		PrecipProb []float64 `json:"precipitation_probability_max"`
	} `json:"daily"`
}

// Geocode resolves a city name to coordinates. Russian speech naturally uses
// inflected forms ("в Москве", "в Казани"), while the geocoder expects the
// nominative form, so try a few case-normalized candidates and prefer an exact
// name match.
func (c *Client) Geocode(ctx context.Context, city string) (Location, error) {
	city = strings.TrimSpace(city)
	if city == "" {
		return Location{}, fmt.Errorf("city is empty")
	}

	var fallback *Location
	for _, candidate := range cityCandidates(city) {
		location, err := c.geocodeOne(ctx, candidate)
		if err != nil {
			continue
		}
		if sameCityName(location.Name, candidate) {
			return location, nil
		}
		if fallback == nil || (fallback.Country != "Россия" && location.Country == "Россия") {
			copied := location
			fallback = &copied
		}
	}
	if fallback != nil {
		return *fallback, nil
	}
	return Location{}, fmt.Errorf("city not found: %s", city)
}

func (c *Client) geocodeOne(ctx context.Context, city string) (Location, error) {
	base := c.GeoURL
	if base == "" {
		base = defaultGeoURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return Location{}, fmt.Errorf("parse geocoding url: %w", err)
	}
	q := u.Query()
	q.Set("name", city)
	q.Set("count", "1")
	q.Set("language", "ru")
	q.Set("format", "json")
	u.RawQuery = q.Encode()

	var response geoResponse
	if err := c.getJSON(ctx, u.String(), &response); err != nil {
		return Location{}, err
	}
	if len(response.Results) == 0 {
		return Location{}, fmt.Errorf("city not found: %s", city)
	}
	r := response.Results[0]
	return Location{
		Name:      r.Name,
		Country:   r.Country,
		Admin1:    r.Admin1,
		Latitude:  r.Latitude,
		Longitude: r.Longitude,
		Timezone:  r.Timezone,
	}, nil
}

// cityCandidates returns the original city plus common Russian case endings
// converted back to a form the geocoder can resolve.
func cityCandidates(city string) []string {
	city = strings.TrimSpace(city)
	if city == "" {
		return nil
	}
	lower := strings.ToLower(city)
	runes := []rune(city)
	add := func(result []string, candidate string) []string {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			return result
		}
		for _, existing := range result {
			if strings.EqualFold(existing, candidate) {
				return result
			}
		}
		return append(result, candidate)
	}
	result := []string{city}
	switch {
	case strings.HasSuffix(lower, "е"):
		// Try the feminine nominative first ("Самаре" -> "Самара"), then the
		// soft-sign form ("Симферополе" -> "Симферополь"), then simple removal
		// ("Екатеринбурге" -> "Екатеринбург").
		result = add(result, string(runes[:len(runes)-1])+"а")
		result = add(result, string(runes[:len(runes)-1])+"ь")
		result = add(result, string(runes[:len(runes)-1]))
	case strings.HasSuffix(lower, "и"):
		result = add(result, string(runes[:len(runes)-1])+"ь")
		result = add(result, string(runes[:len(runes)-1]))
		result = add(result, string(runes[:len(runes)-1])+"я")
	case strings.HasSuffix(lower, "у"):
		result = add(result, string(runes[:len(runes)-1])+"а")
	case strings.HasSuffix(lower, "ю"):
		result = add(result, string(runes[:len(runes)-1])+"я")
	case strings.HasSuffix(lower, "ой"), strings.HasSuffix(lower, "ей"):
		result = add(result, string(runes[:len(runes)-2])+"а")
	case strings.HasSuffix(lower, "ом"), strings.HasSuffix(lower, "ем"):
		result = add(result, string(runes[:len(runes)-2]))
	case strings.HasSuffix(lower, "ы"):
		result = add(result, string(runes[:len(runes)-1])+"а")
	}
	if lower == "орле" {
		result = add(result, "орёл")
	}
	return result
}

func sameCityName(name, candidate string) bool {
	normalize := func(value string) string {
		value = strings.ToLower(strings.TrimSpace(value))
		value = strings.ReplaceAll(value, "ё", "е")
		value = strings.ReplaceAll(value, "-", " ")
		return strings.Join(strings.Fields(value), " ")
	}
	return normalize(name) == normalize(candidate)
}

// LocateIP resolves an approximate location from the public IP address.
func (c *Client) LocateIP(ctx context.Context) (Location, error) {
	base := c.IPURL
	if base == "" {
		base = defaultIPURL
	}
	var response ipResponse
	if err := c.getJSON(ctx, base, &response); err != nil {
		return Location{}, err
	}
	if !response.Success || response.City == "" {
		return Location{}, fmt.Errorf("IP geolocation did not return a city")
	}
	// The IP database returns English names. Geocode the city in Russian so
	// speech sounds natural and the coordinates come from Open-Meteo.
	if location, err := c.Geocode(ctx, response.City); err == nil {
		return location, nil
	}
	return Location{
		Name:      response.City,
		Country:   response.Country,
		Latitude:  response.Latitude,
		Longitude: response.Longitude,
		Timezone:  response.Timezone.ID,
	}, nil
}

// Get fetches a forecast for a city or, when city is empty, for the IP-based
// approximate location. period is now, today, tomorrow or after_tomorrow.
func (c *Client) Get(ctx context.Context, city, period string) (Forecast, error) {
	var (
		loc Location
		err error
	)
	if strings.TrimSpace(city) == "" {
		loc, err = c.LocateIP(ctx)
	} else {
		loc, err = c.Geocode(ctx, city)
	}
	if err != nil {
		return Forecast{}, err
	}
	return c.GetForLocation(ctx, loc, period)
}

// GetForLocation fetches a forecast for already resolved coordinates.
func (c *Client) GetForLocation(ctx context.Context, loc Location, period string) (Forecast, error) {
	if period == "" {
		period = "today"
	}
	base := c.ForecastURL
	if base == "" {
		base = defaultForecastURL
	}
	u, err := url.Parse(base)
	if err != nil {
		return Forecast{}, fmt.Errorf("parse forecast url: %w", err)
	}
	q := u.Query()
	q.Set("latitude", strconv.FormatFloat(loc.Latitude, 'f', 6, 64))
	q.Set("longitude", strconv.FormatFloat(loc.Longitude, 'f', 6, 64))
	q.Set("current", "temperature_2m,apparent_temperature,relative_humidity_2m,weather_code,wind_speed_10m")
	q.Set("daily", "weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max")
	q.Set("timezone", "auto")
	q.Set("forecast_days", "4")
	u.RawQuery = q.Encode()

	var response forecastResponse
	if err := c.getJSON(ctx, u.String(), &response); err != nil {
		return Forecast{}, err
	}

	f := Forecast{
		Location: loc,
		Period:   period,
		Current: Current{
			Time:        response.Current.Time,
			Temperature: response.Current.Temperature,
			Apparent:    response.Current.Apparent,
			Humidity:    response.Current.Humidity,
			Wind:        response.Current.Wind,
			Code:        response.Current.WeatherCode,
		},
	}
	for i := range response.Daily.Time {
		d := Daily{Date: response.Daily.Time[i]}
		if i < len(response.Daily.Code) {
			d.Code = response.Daily.Code[i]
		}
		if i < len(response.Daily.Max) {
			d.Max = response.Daily.Max[i]
		}
		if i < len(response.Daily.Min) {
			d.Min = response.Daily.Min[i]
		}
		if i < len(response.Daily.PrecipProb) {
			d.PrecipProb = response.Daily.PrecipProb[i]
		}
		f.Daily = append(f.Daily, d)
	}
	if len(f.Daily) == 0 {
		return Forecast{}, fmt.Errorf("forecast has no daily data")
	}
	return f, nil
}

func (c *Client) getJSON(ctx context.Context, rawURL string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("create weather request: %w", err)
	}
	req.Header.Set("User-Agent", "Jarvis/1.0 (local desktop assistant)")
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("weather request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("weather service returned %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxWeatherResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read weather response: %w", err)
	}
	if len(data) > maxWeatherResponseBytes {
		return fmt.Errorf("weather response is too large")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("decode weather response: %w", err)
	}
	return nil
}

// Day returns the requested daily forecast. The bool is false when the day is
// not present in the response.
func (f Forecast) Day(period string) (Daily, bool) {
	index := 0
	switch period {
	case "tomorrow":
		index = 1
	case "after_tomorrow":
		index = 2
	}
	if index >= len(f.Daily) {
		return Daily{}, false
	}
	return f.Daily[index], true
}

// Markdown returns a compact card-friendly Russian forecast.
func (f Forecast) Markdown() string {
	city := f.Location.Name
	if city == "" {
		city = "Ваш город"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### Погода: %s\n\n", city)
	fmt.Fprintf(&b, "**Сейчас:** %s, %s  \n", temp(f.Current.Temperature), description(f.Current.Code))
	fmt.Fprintf(&b, "Ощущается как %s · влажность %d%% · ветер %s\n", temp(f.Current.Apparent), int(f.Current.Humidity+0.5), wind(f.Current.Wind))

	labels := []string{"Сегодня", "Завтра", "Послезавтра", "Через 3 дня"}
	for i, day := range f.Daily {
		if i >= len(labels) {
			break
		}
		fmt.Fprintf(&b, "\n**%s:** %s … %s, %s", labels[i], temp(day.Min), temp(day.Max), description(day.Code))
		if day.PrecipProb > 0 {
			fmt.Fprintf(&b, " · осадки %d%%", int(day.PrecipProb+0.5))
		}
	}
	return b.String()
}

func temp(value float64) string {
	rounded := int(value + 0.5)
	if value < 0 {
		rounded = int(value - 0.5)
	}
	if rounded > 0 {
		return fmt.Sprintf("+%d°", rounded)
	}
	return fmt.Sprintf("%d°", rounded)
}

func wind(value float64) string {
	return fmt.Sprintf("%d м/с", int(value+0.5))
}

func description(code int) string {
	switch code {
	case 0:
		return "ясно"
	case 1:
		return "в основном ясно"
	case 2:
		return "переменная облачность"
	case 3:
		return "пасмурно"
	case 45, 48:
		return "туман"
	case 51, 53, 55:
		return "морось"
	case 56, 57:
		return "ледяная морось"
	case 61, 63, 65:
		return "дождь"
	case 66, 67:
		return "ледяной дождь"
	case 71, 73, 75:
		return "снег"
	case 77:
		return "снежные зёрна"
	case 80, 81, 82:
		return "ливневый дождь"
	case 85, 86:
		return "ливневый снег"
	case 95:
		return "гроза"
	case 96, 99:
		return "гроза с градом"
	default:
		return "без осадков"
	}
}
