// Package stockticker provides the "quotes" data source for the widget.
package stockticker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"

	dw "github.com/cdro/desktopwidgets-core/runtime"
)

// Quote is what the frontend receives per symbol.
type Quote struct {
	Symbol        string    `json:"symbol"`
	Price         float64   `json:"price"`
	Change        float64   `json:"change"`
	ChangePercent float64   `json:"changePercent"`
	Currency      string    `json:"currency,omitempty"`
	Spark         []float64 `json:"spark,omitempty"` // intraday closes, oldest first
	Error         string    `json:"error,omitempty"`
}

// Overridable in tests.
var (
	yahooBase   = "https://query1.finance.yahoo.com"
	finnhubBase = "https://finnhub.io"
)

func init() {
	dw.Provide("quotes", func(ctx *dw.Ctx) (any, error) {
		symbols := ParseSymbols(ctx.String("symbols"))
		if len(symbols) == 0 {
			return nil, errors.New("no symbols configured")
		}
		fetch := func(c context.Context, s string) (Quote, error) { return yahoo(c, ctx.HTTP, s) }
		if ctx.String("source") == "finnhub" {
			key := ctx.String("apiKey")
			if key == "" {
				return nil, errors.New("Finnhub needs an API key (see settings)")
			}
			fetch = func(c context.Context, s string) (Quote, error) { return finnhub(c, ctx.HTTP, key, s) }
		}
		return FetchAll(ctx, symbols, fetch)
	})
}

// ParseSymbols splits "AAPL, msft,,^GSPC" into ["AAPL", "MSFT", "^GSPC"].
func ParseSymbols(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		p = strings.ToUpper(strings.TrimSpace(p))
		if p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// FetchAll queries all symbols concurrently. Per-symbol failures are
// reported in Quote.Error; it only fails if every symbol failed.
func FetchAll(ctx context.Context, symbols []string, fetch func(context.Context, string) (Quote, error)) ([]Quote, error) {
	out := make([]Quote, len(symbols))
	var wg sync.WaitGroup
	for i, s := range symbols {
		wg.Add(1)
		go func(i int, s string) {
			defer wg.Done()
			q, err := fetch(ctx, s)
			if err != nil {
				q = Quote{Symbol: s, Error: err.Error()}
			}
			out[i] = q
		}(i, s)
	}
	wg.Wait()
	for _, q := range out {
		if q.Error == "" {
			return out, nil
		}
	}
	return nil, fmt.Errorf("all symbols failed: %s", out[0].Error)
}

func getJSON(ctx context.Context, client *http.Client, u string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (DesktopWidgets stock-ticker)")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}

type yahooChart struct {
	Chart struct {
		Result []struct {
			Meta struct {
				Symbol             string  `json:"symbol"`
				Currency           string  `json:"currency"`
				RegularMarketPrice float64 `json:"regularMarketPrice"`
				ChartPreviousClose float64 `json:"chartPreviousClose"`
				PreviousClose      float64 `json:"previousClose"`
			} `json:"meta"`
			Indicators struct {
				Quote []struct {
					Close []*float64 `json:"close"`
				} `json:"quote"`
			} `json:"indicators"`
		} `json:"result"`
		Error *struct {
			Description string `json:"description"`
		} `json:"error"`
	} `json:"chart"`
}

func yahoo(ctx context.Context, client *http.Client, symbol string) (Quote, error) {
	u := yahooBase + "/v8/finance/chart/" + url.PathEscape(symbol) + "?interval=5m&range=1d"
	var c yahooChart
	if err := getJSON(ctx, client, u, &c); err != nil {
		return Quote{}, err
	}
	if c.Chart.Error != nil {
		return Quote{}, errors.New(c.Chart.Error.Description)
	}
	if len(c.Chart.Result) == 0 {
		return Quote{}, errors.New("unknown symbol")
	}
	r := c.Chart.Result[0]
	prev := r.Meta.ChartPreviousClose
	if prev == 0 {
		prev = r.Meta.PreviousClose
	}
	q := Quote{Symbol: symbol, Price: r.Meta.RegularMarketPrice, Currency: r.Meta.Currency}
	if prev != 0 {
		q.Change = q.Price - prev
		q.ChangePercent = q.Change / prev * 100
	}
	if len(r.Indicators.Quote) > 0 {
		for _, v := range r.Indicators.Quote[0].Close {
			if v != nil {
				q.Spark = append(q.Spark, *v)
			}
		}
	}
	return q, nil
}

type finnhubQuote struct {
	C  float64 `json:"c"`
	D  float64 `json:"d"`
	DP float64 `json:"dp"`
}

func finnhub(ctx context.Context, client *http.Client, key, symbol string) (Quote, error) {
	u := finnhubBase + "/api/v1/quote?symbol=" + url.QueryEscape(symbol) + "&token=" + url.QueryEscape(key)
	var f finnhubQuote
	if err := getJSON(ctx, client, u, &f); err != nil {
		return Quote{}, err
	}
	if f.C == 0 && f.D == 0 {
		return Quote{}, errors.New("unknown symbol")
	}
	return Quote{Symbol: symbol, Price: f.C, Change: f.D, ChangePercent: f.DP}, nil
}
