package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/gorilla/websocket"
)

const BinanceSpotDepthBaseURL = "wss://stream.binance.com:9443/ws"

type BinanceDepthConfig struct {
	URL           string
	StreamSymbol  string
	DomainSymbol  domain.Symbol
	PriceScale    int64
	QuantityScale int64
}

type BinanceDepthFeed struct {
	config BinanceDepthConfig
	conn   *websocket.Conn
}

func NewBinanceDepthFeed(ctx context.Context, config BinanceDepthConfig) (*BinanceDepthFeed, error) {
	feed := &BinanceDepthFeed{config: normalizeBinanceConfig(config)}
	if err := feed.Reconnect(ctx); err != nil {
		return nil, err
	}
	return feed, nil
}

func (f *BinanceDepthFeed) Next(ctx context.Context) (Message, error) {
	type readResult struct {
		payload []byte
		err     error
	}
	ch := make(chan readResult, 1)
	go func() {
		_, payload, err := f.conn.ReadMessage()
		ch <- readResult{payload: payload, err: err}
	}()

	select {
	case <-ctx.Done():
		return Message{}, ctx.Err()
	case result := <-ch:
		if result.err != nil {
			return Message{}, result.err
		}
		return f.parse(result.payload)
	}
}

func (f *BinanceDepthFeed) Reconnect(ctx context.Context) error {
	if f.conn != nil {
		_ = f.conn.Close()
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, f.streamURL(), nil)
	if err != nil {
		return err
	}
	f.conn = conn
	return nil
}

func (f *BinanceDepthFeed) Close() error {
	if f.conn == nil {
		return nil
	}
	return f.conn.Close()
}

func (f *BinanceDepthFeed) streamURL() string {
	base := strings.TrimRight(f.config.URL, "/")
	return fmt.Sprintf("%s/%s@depth", base, strings.ToLower(f.config.StreamSymbol))
}

func (f *BinanceDepthFeed) parse(payload []byte) (Message, error) {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return Message{}, ErrInvalidUpdate
	}
	if data, ok := envelope["data"]; ok {
		return f.parse(data)
	}

	lastUpdateID, _ := rawInt64(envelope, "lastUpdateId")
	if lastUpdateID > 0 {
		rawBids, err := rawStringLevels(envelope, "bids")
		if err != nil {
			return Message{}, err
		}
		rawAsks, err := rawStringLevels(envelope, "asks")
		if err != nil {
			return Message{}, err
		}
		bids, err := f.parseLevels(rawBids)
		if err != nil {
			return Message{}, err
		}
		asks, err := f.parseLevels(rawAsks)
		if err != nil {
			return Message{}, err
		}
		return Message{
			Type: MessageTypeSnapshot,
			Snapshot: &Snapshot{
				Symbol:   f.config.DomainSymbol,
				Sequence: domain.Sequence(lastUpdateID),
				Bids:     bids,
				Asks:     asks,
			},
		}, nil
	}

	eventType, _ := rawString(envelope, "e")
	firstUpdate, _ := rawInt64(envelope, "U")
	finalUpdate, _ := rawInt64(envelope, "u")
	if eventType == "depthUpdate" && finalUpdate > 0 {
		rawBids, err := rawStringLevels(envelope, "b")
		if err != nil {
			return Message{}, err
		}
		rawAsks, err := rawStringLevels(envelope, "a")
		if err != nil {
			return Message{}, err
		}
		bids, err := f.parseLevels(rawBids)
		if err != nil {
			return Message{}, err
		}
		asks, err := f.parseLevels(rawAsks)
		if err != nil {
			return Message{}, err
		}
		return Message{
			Type: MessageTypeUpdate,
			Update: &Update{
				Symbol:        f.config.DomainSymbol,
				FirstSequence: domain.Sequence(firstUpdate),
				Sequence:      domain.Sequence(finalUpdate),
				Bids:          bids,
				Asks:          asks,
			},
		}, nil
	}

	return Message{}, ErrUnknownMessage
}

func rawString(envelope map[string]json.RawMessage, key string) (string, error) {
	raw, ok := envelope[key]
	if !ok {
		return "", ErrInvalidUpdate
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", ErrInvalidUpdate
	}
	return value, nil
}

func rawInt64(envelope map[string]json.RawMessage, key string) (int64, error) {
	raw, ok := envelope[key]
	if !ok {
		return 0, ErrInvalidUpdate
	}
	var value int64
	if err := json.Unmarshal(raw, &value); err != nil {
		return 0, ErrInvalidUpdate
	}
	return value, nil
}

func rawStringLevels(envelope map[string]json.RawMessage, key string) ([][]string, error) {
	raw, ok := envelope[key]
	if !ok {
		return nil, ErrInvalidUpdate
	}
	var levels [][]string
	if err := json.Unmarshal(raw, &levels); err != nil {
		return nil, ErrInvalidUpdate
	}
	return levels, nil
}

func (f *BinanceDepthFeed) parseLevels(raw [][]string) ([]Level, error) {
	levels := make([]Level, 0, len(raw))
	for _, rawLevel := range raw {
		if len(rawLevel) != 2 {
			return nil, ErrInvalidUpdate
		}
		price, err := parseScaledDecimal(rawLevel[0], f.config.PriceScale)
		if err != nil {
			return nil, ErrInvalidUpdate
		}
		quantity, err := parseScaledDecimal(rawLevel[1], f.config.QuantityScale)
		if err != nil {
			return nil, ErrInvalidUpdate
		}
		levels = append(levels, Level{
			Price:    domain.Money(price),
			Quantity: domain.Quantity(quantity),
		})
	}
	return levels, nil
}

func normalizeBinanceConfig(config BinanceDepthConfig) BinanceDepthConfig {
	if config.URL == "" {
		config.URL = BinanceSpotDepthBaseURL
	}
	if config.StreamSymbol == "" {
		config.StreamSymbol = "btcusdt"
	}
	if config.DomainSymbol == "" {
		config.DomainSymbol = domain.Symbol(strings.ToUpper(config.StreamSymbol))
	}
	if config.PriceScale == 0 {
		config.PriceScale = 100
	}
	if config.QuantityScale == 0 {
		config.QuantityScale = 100_000_000
	}
	return config
}

func parseScaledDecimal(value string, scale int64) (int64, error) {
	if value == "" || scale <= 0 {
		return 0, ErrInvalidUpdate
	}

	parts := strings.Split(value, ".")
	if len(parts) > 2 || parts[0] == "" {
		return 0, ErrInvalidUpdate
	}

	whole, err := parseDigits(parts[0])
	if err != nil {
		return 0, err
	}
	result := whole * scale

	if len(parts) == 1 {
		return result, nil
	}

	divisor := int64(10)
	for _, digit := range parts[1] {
		if digit < '0' || digit > '9' {
			return 0, ErrInvalidUpdate
		}
		if divisor > scale {
			continue
		}
		result += int64(digit-'0') * (scale / divisor)
		if divisor > scale/10 {
			divisor = scale + 1
			continue
		}
		divisor *= 10
	}
	return result, nil
}

func parseDigits(value string) (int64, error) {
	var result int64
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, ErrInvalidUpdate
		}
		result = result*10 + int64(digit-'0')
	}
	return result, nil
}
