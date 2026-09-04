package mcp

import (
	"context"
	"strings"

	"github.com/digiogithub/git-in-track/internal/core"
)

// The board tool. Moving a card is the one operation that spans two
// repositories — the item's status in its project clone and the order list in
// the team repository — so it is the workspace, not a single vault, that
// answers it (docs/04 R-MOVE-1).

// MoveOnBoardInput moves one card between the columns of a team board.
type MoveOnBoardInput struct {
	Board    string `json:"board" jsonschema:"Board id, for example platform-kanban"`
	Ref      string `json:"ref" jsonschema:"Card reference as <projectKey>/<itemId>, for example ACME/ACME-T-0311"`
	ToColumn string `json:"toColumn" jsonschema:"Target column id declared by the board"`
	Position int    `json:"position,omitempty" jsonschema:"0-based index in the target column; -1 appends"`
	Status   string `json:"status,omitempty" jsonschema:"Status to set when the column maps several"`
	Rev      string `json:"rev,omitempty" jsonschema:"Board rev from the read this move is based on"`
	ItemRev  string `json:"itemRev,omitempty" jsonschema:"Item rev from the read this move is based on"`
	Force    bool   `json:"force,omitempty" jsonschema:"Confirm a move that exceeds the column's WIP limit"`
}

// MoveResult is what a card move answers with: what the move implied, the item
// as it now stands and the files that changed in each repository.
type MoveResult struct {
	Ref           string   `json:"ref"`
	FromColumn    string   `json:"fromColumn,omitempty"`
	ToColumn      string   `json:"toColumn"`
	Status        string   `json:"status,omitempty"`
	StatusChanged bool     `json:"statusChanged"`
	WIPUsed       int      `json:"wipUsed,omitempty"`
	WIPLimit      int      `json:"wipLimit,omitempty"`
	Item          *Item    `json:"item,omitempty"`
	Changed       []string `json:"changed,omitempty" jsonschema:"Vault-relative paths written, across every repository"`
}

// registerBoardTools declares the board half of the surface.
func registerBoardTools(s *Server) {
	register(s, toolDef{
		Name:  "move_on_board",
		Title: "Move a card on a team board",
		Description: "Move one card to another column of a team board. This writes two files in two " +
			"repositories — the item's status in its project clone and the column order in the team " +
			"repository — and validates the transition against the project workflow and the column's " +
			"WIP limit. A card whose project is not cloned cannot be moved.",
		Write: true,
	}, moveOnBoard)
}

// moveOnBoard delegates the whole move to the workspace, which owns the
// two-repository ordering and the rollback if the second write fails.
func moveOnBoard(ctx context.Context, s *Server, in MoveOnBoardInput) (MoveResult, error) {
	if strings.TrimSpace(in.Board) == "" {
		return MoveResult{}, invalidField("board", "move_on_board needs a board id", "platform-kanban")
	}
	if strings.TrimSpace(in.Ref) == "" {
		return MoveResult{}, invalidField("ref", "move_on_board needs a card reference",
			"ACME/ACME-T-0311")
	}
	if strings.TrimSpace(in.ToColumn) == "" {
		return MoveResult{}, invalidField("toColumn", "move_on_board needs a target column", "in_review")
	}
	result, err := s.dispatchRaw(ctx, "board.move", map[string]any{
		"board": in.Board, "ref": in.Ref, "toColumn": in.ToColumn,
		"position": in.Position, "status": in.Status,
		"rev": in.Rev, "itemRev": in.ItemRev, "force": in.Force,
	})
	if err != nil {
		return MoveResult{}, err
	}
	payload, err := decodeResult[boardMovePayload](result)
	if err != nil {
		return MoveResult{}, err
	}
	out := MoveResult{
		Ref:           payload.Move.Ref,
		FromColumn:    payload.Move.FromColumn,
		ToColumn:      payload.Move.ToColumn,
		Status:        payload.Move.Status,
		StatusChanged: payload.Move.StatusChanged,
		WIPUsed:       payload.Move.WIP.Used,
		WIPLimit:      payload.Move.WIP.Limit,
	}
	for _, set := range payload.Writes {
		for _, f := range set.Written {
			out.Changed = append(out.Changed, f.Path)
		}
		out.Changed = append(out.Changed, set.Removed...)
	}
	itemID := ""
	if payload.Item != nil {
		item := itemOf(*payload.Item)
		item.Body = ""
		projected := projectItem(item, nil)
		out.Item = &projected
		itemID = projected.ID
	}
	s.announce(ctx, WriteEvent{
		Tool: "move_on_board", Method: "board.move",
		ItemID: itemID, Op: "moved", Result: result,
	})
	return out, nil
}

// boardMovePayload is the half of the core's board.move answer this tool
// publishes. The board view itself is deliberately not returned: it is large,
// and an agent that wants it calls the board read.
type boardMovePayload struct {
	Item *core.Item `json:"item"`
	Move struct {
		Ref           string `json:"ref"`
		FromColumn    string `json:"fromColumn"`
		ToColumn      string `json:"toColumn"`
		Status        string `json:"status"`
		StatusChanged bool   `json:"statusChanged"`
		WIP           struct {
			Used  int `json:"used"`
			Limit int `json:"limit"`
		} `json:"wip"`
	} `json:"move"`
	Writes []struct {
		Written []struct {
			Path string `json:"path"`
		} `json:"written"`
		Removed []string `json:"removed"`
	} `json:"writes"`
}
