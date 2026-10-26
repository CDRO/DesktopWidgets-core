package stockticker

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestParseSymbols(t *testing.T) {
	got := ParseSymbols(" aapl, MSFT,,^gspc , aapl")
	want := []string{"AAPL", "MSFT", "^GSPC"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}

const yahooFixture = `{"chart":{"result":[{"meta":{"currency":"USD","symbol":"AAPL","regularMarketPrice":110.0,"chartPreviousClose":100.0},
"indicators":{"quote":[{"close":[101.5,null,109.25,110.0]}]}}],"error":null}}`

func TestYahoo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v8/finance/chart/AAPL") {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(yahooFixture))
	}))
	defer srv.Close()
	yahooBase = srv.URL

	q, err := yahoo(context.Background(), srv.Client(), "AAPL")
	if err != nil {
		t.Fatal(err)
	}
	if q.Price != 110 || q.Change != 10 || math.Abs(q.ChangePercent-10) > 1e-9 || q.Currency != "USD" {
		t.Fatalf("unexpected quote %+v", q)
	}
	if !reflect.DeepEqual(q.Spark, []float64{101.5, 109.25, 110}) {
		t.Fatalf("spark: %v", q.Spark)
	}
	if _, err := yahoo(context.Background(), srv.Client(), "NOPE"); err == nil {
		t.Fatal("expected error for unknown symbol")
	}
}

func TestFinnhub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "k" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte(`{"c":50,"d":-2.5,"dp":-4.76,"pc":52.5}`))
	}))
	defer srv.Close()
	finnhubBase = srv.URL

	q, err := finnhub(context.Background(), srv.Client(), "k", "MSFT")
	if err != nil || q.Price != 50 || q.Change != -2.5 {
		t.Fatalf("got %+v, %v", q, err)
	}
	if _, err := finnhub(context.Background(), srv.Client(), "bad", "MSFT"); err == nil {
		t.Fatal("expected auth error")
	}
}

func TestFetchAllPartialFailure(t *testing.T) {
	fetch := func(_ context.Context, s string) (Quote, error) {
		if s == "BAD" {
			return Quote{}, context.DeadlineExceeded
		}
		return Quote{Symbol: s, Price: 1}, nil
	}
	got, err := FetchAll(context.Background(), []string{"A", "BAD"}, fetch)
	if err != nil || got[0].Price != 1 || got[1].Error == "" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := FetchAll(context.Background(), []string{"BAD"}, fetch); err == nil {
		t.Fatal("expected error when all symbols fail")
	}
}
