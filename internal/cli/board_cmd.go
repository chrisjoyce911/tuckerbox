package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/chrisjoyce911/tuckerbox/internal/board"
	"github.com/chrisjoyce911/tuckerbox/internal/statedir"
)

func (a App) board(args []string) int {
	var asJSON bool
	var change string
	pos, err := parseArgs(args, map[string]*bool{"json": &asJSON}, map[string]*string{"change": &change})
	if err != nil || len(pos) != 0 {
		return a.usageErr("board", "usage: tuckerbox board [--json] [--change <id>]")
	}
	if change != "" {
		if err := statedir.CheckID("change", change); err != nil {
			return a.usageErr("board", err.Error())
		}
	}
	env, getwd := a.configEnv()
	dir, err := getwd()
	if err != nil {
		return a.errf("board", "%v", err)
	}
	doc, err := board.Read(context.Background(), dir, change, env)
	if err != nil {
		if asJSON {
			code := "board_error"
			var be *board.Error
			if errors.As(err, &be) {
				code = be.Code
			}
			if a.writeJSON(map[string]any{"version": board.Version, "error": map[string]string{"code": code, "message": err.Error()}}) != 0 {
				return 1
			}
			return 1
		}
		return a.errf("board", "%v", err)
	}
	if asJSON {
		return a.writeJSON(doc)
	}
	rows := [][]string{{"CHANGE", "TITLE", "COLUMN", "BADGE", "TASKS", "STATE", "REASON", "ERROR"}}
	for _, c := range doc.Cards {
		rows = append(rows, []string{c.Change, c.Title, c.Column, c.Badge, strconv.Itoa(c.Tasks.Done) + "/" + strconv.Itoa(c.Tasks.Total), dash(c.State), c.Reason, c.Error})
	}
	writeTable(a.Stdout, rows)
	if doc.Detail != nil {
		fmt.Fprintln(a.Stdout, "\n"+doc.Detail.Proposal)
	}
	return 0
}
