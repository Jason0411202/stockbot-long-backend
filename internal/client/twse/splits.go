package twse

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Jason0411202/stockbot-long-backend/internal/marketunits"
)

type SplitClient struct {
	HTTP    *http.Client
	BaseURL string
}

func NewSplitClient() *SplitClient {
	return &SplitClient{HTTP: &http.Client{Timeout: 25 * time.Second}, BaseURL: "https://www.twse.com.tw/rwd/zh/split/"}
}

type splitResponse struct {
	Stat   string     `json:"stat"`
	Data   [][]string `json:"data"`
	Fields []string   `json:"fields"`
}

func splitDate(s string) (string, error) {
	parts := strings.Split(strings.TrimSpace(s), "/")
	if len(parts) != 3 {
		return "", fmt.Errorf("invalid TWSE action date %q", s)
	}
	y, err := strconv.Atoi(parts[0])
	if err != nil {
		return "", err
	}
	if y < 1911 {
		y += 1911
	}
	d := fmt.Sprintf("%04d-%s-%s", y, parts[1], parts[2])
	_, err = time.Parse("2006-01-02", d)
	return d, err
}

func splitNumber(s string) (float64, error) {
	return strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(s), ",", ""), 64)
}

// Historical reference prices are exchange reference prices, NOT the resumed
// session's open/close. Recover the integral ratio subject to tick rounding.
func referenceRatio(before, reference float64) (float64, error) {
	if before <= 0 || reference <= 0 {
		return 0, fmt.Errorf("invalid split reference prices")
	}
	ratio := math.Round(before / reference)
	if before < reference {
		ratio = 1 / math.Round(reference/before)
	}
	if ratio <= 0 || ratio == 1 || math.Abs(before/ratio-reference) > math.Max(0.011, reference*0.0001) {
		return 0, fmt.Errorf("inconsistent split reference prices %.8f / %.8f", before, reference)
	}
	return ratio, nil
}

func (c *SplitClient) table(ctx context.Context, path string, q url.Values) (splitResponse, error) {
	q.Set("response", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return splitResponse{}, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return splitResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return splitResponse{}, fmt.Errorf("TWSE %s HTTP %d", path, resp.StatusCode)
	}
	var table splitResponse
	if err := json.NewDecoder(resp.Body).Decode(&table); err != nil {
		return table, err
	}
	if table.Stat != "OK" {
		if len(table.Data) == 0 && strings.Contains(table.Stat, "沒有符合") {
			return table, nil
		}
		return table, fmt.Errorf("TWSE %s: %s", path, table.Stat)
	}
	return table, nil
}

func (c *SplitClient) FetchSplits(ctx context.Context) ([]marketunits.Action, error) {
	end := time.Now().In(time.FixedZone("Asia/Taipei", 8*3600)).Format("20060102")
	history, err := c.table(ctx, "TWTCAU", url.Values{"startDate": {"20100101"}, "endDate": {end}})
	if err != nil {
		return nil, err
	}
	out := make([]marketunits.Action, 0, len(history.Data))
	for _, row := range history.Data {
		if len(row) < 6 {
			return nil, fmt.Errorf("short TWSE split reference row")
		}
		date, err := splitDate(row[0])
		if err != nil {
			return nil, err
		}
		before, err := splitNumber(row[4])
		if err != nil {
			return nil, err
		}
		after, err := splitNumber(row[5])
		if err != nil {
			return nil, err
		}
		ratio, err := referenceRatio(before, after)
		if err != nil {
			return nil, err
		}
		out = append(out, marketunits.Action{StockID: strings.TrimSpace(row[1]), Date: date, Ratio: ratio})
	}
	announced, err := c.table(ctx, "TWTC9U", url.Values{})
	if err != nil {
		return nil, err
	}
	for _, row := range announced.Data {
		if len(row) < 6 {
			return nil, fmt.Errorf("short TWSE split announcement row")
		}
		resume, err := splitDate(row[4])
		if err != nil {
			return nil, err
		}
		suspend, err := splitDate(row[0])
		if err != nil {
			return nil, err
		}
		ratio, err := splitNumber(row[5])
		if err != nil {
			return nil, err
		}
		out = append(out, marketunits.Action{StockID: strings.TrimSpace(row[1]), Date: resume, SuspendFrom: suspend, Ratio: ratio})
	}
	return out, nil
}
