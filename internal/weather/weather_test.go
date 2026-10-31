package weather

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCityCandidates(t *testing.T) {
	cases := map[string]string{
		"Москве":           "Москва",
		"Казани":           "Казань",
		"Самаре":           "Самара",
		"Симферополе":      "Симферополь",
		"Екатеринбурге":    "Екатеринбург",
		"Лондоне":          "Лондон",
		"Минске":           "Минск",
		"Москвой":          "Москва",
		"Санкт-Петербурге": "Санкт-Петербург",
	}
	for input, expected := range cases {
		found := false
		for _, candidate := range cityCandidates(input) {
			if candidate == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("cityCandidates(%q) = %v, missing %q", input, cityCandidates(input), expected)
		}
	}
}

func TestSameCityName(t *testing.T) {
	if !sameCityName("Санкт-Петербург", "санкт петербург") {
		t.Fatal("expected normalized names to match")
	}
	if sameCityName("Москверо", "Москве") {
		t.Fatal("prefix must not be treated as an exact match")
	}
}

func TestGetJSONRejectsOversizedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":["`))
		_, _ = w.Write([]byte(strings.Repeat("x", maxWeatherResponseBytes)))
		_, _ = w.Write([]byte(`"]}`))
	}))
	defer server.Close()

	client := &Client{HTTP: server.Client()}
	var target any
	if err := client.getJSON(t.Context(), server.URL, &target); err == nil {
		t.Fatal("expected an oversized response to be rejected")
	} else if !strings.Contains(err.Error(), "too large") {
		t.Fatalf("unexpected error: %v", err)
	}
}
