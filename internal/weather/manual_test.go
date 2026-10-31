//go:build manual

package weather

import (
	"context"
	"testing"
	"time"
)

func TestManualWeather(t *testing.T) {
	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	forecast, err := client.Get(ctx, "Москва", "today")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("location=%s", forecast.Location.Name)
	t.Logf("markdown=%s", forecast.Markdown())
}

func TestManualGeocodeCases(t *testing.T) {
	client := NewClient()
	for _, city := range []string{"Москве", "Казани", "Самаре", "Уфе", "Екатеринбурге", "Лондоне", "Минске"} {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		location, err := client.Geocode(ctx, city)
		cancel()
		if err != nil {
			t.Errorf("%s: %v", city, err)
			continue
		}
		t.Logf("%s -> %s, %s (%.4f, %.4f)", city, location.Name, location.Country, location.Latitude, location.Longitude)
	}
}

func TestManualIPLocation(t *testing.T) {
	client := NewClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	location, err := client.LocateIP(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("ip location=%+v", location)
}
