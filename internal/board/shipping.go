package board

import (
	"os"

	"github.com/chrisjoyce911/tuckerbox/internal/config"
	"github.com/chrisjoyce911/tuckerbox/internal/runs"
)

func shippingFolder(root, id, fallback string) string {
	if _, err := os.Stat(fallback); err == nil {
		return fallback
	}
	folders, err := archiveFolders(root)
	if err != nil {
		return fallback
	}
	for _, folder := range folders {
		if folder.id == id {
			return folder.folder
		}
	}
	return fallback
}
func addShipping(card *Card, run *runs.Run, cfg *config.Config) {
	if run == nil {
		return
	}
	card.MR, card.CI, card.ShipRound = run.MR, run.CI, run.ShipRound
	if run.ShippingStarted || run.MR != nil {
		card.MaxRounds = cfg.Shipping.MaxRounds
	}
	switch {
	case run.State == runs.Merged:
		card.State = "merged"
		card.Column = "done"
		card.Badge = "merged"
	case run.State == runs.Ready && run.MR != nil && run.MR.State == "open" && cfg.Ship == config.ShipMR:
		card.Badge = "waiting for merge"
	case run.ShippingStarted && run.State != runs.Ready:
		card.Column = "shipping"
		if card.Badge == "" {
			card.Badge = "shipping"
		}
	}
}
