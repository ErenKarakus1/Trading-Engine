package app

import (
	"net/http"
	"os"

	"github.com/ErenKarakus1/Trading-Engine/internal/api"
	"github.com/ErenKarakus1/Trading-Engine/internal/domain"
	"github.com/ErenKarakus1/Trading-Engine/internal/matching"
	"github.com/ErenKarakus1/Trading-Engine/internal/risk"
)

// Run is the application entrypoint.
func Run() error {
	addr := os.Getenv("TRADING_ENGINE_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	server := api.NewServer(
		matching.NewEngine(),
		risk.NewEngine(risk.Config{MaxOrderQuantity: 1_000_000, MaxPosition: 1_000_000}),
		nil,
		[]risk.Account{
			{
				ID:   "demo",
				Cash: 1_000_000_000,
				Positions: map[domain.Symbol]domain.Quantity{
					"BTC-USD": 1_000_000,
					"ETH-USD": 1_000_000,
				},
			},
		},
	)

	return http.ListenAndServe(addr, server.Router())
}
