package sandbox

import (
	"bytes"
	"context"
	"embed"

	"github.com/jackc/pgx/v5"

	"shigoto/internal/core"
	"shigoto/internal/property"
	"shigoto/internal/storage"
)

//go:embed media/*.png media/*.mp4 media/*.m4a
var demoMedia embed.FS

// seedMaintenanceMedia stages tiny playable fixtures for new demo tickets.
// The caller must abort every returned stage on error, including partial results.
func seedMaintenanceMedia(ctx context.Context, tx pgx.Tx, store storage.Store, orgs []core.Org) ([]*core.Staged, error) {
	var stages []*core.Staged
	for _, org := range orgs {
		rows, err := tx.Query(ctx, `SELECT id,raised_by_user_id FROM maintenance_tickets WHERE org_id=$1 AND status='new' ORDER BY id`, org.ID)
		if err != nil {
			return stages, err
		}
		var tickets []struct{ id, userID int64 }
		for rows.Next() {
			var ticket struct{ id, userID int64 }
			if err := rows.Scan(&ticket.id, &ticket.userID); err != nil {
				rows.Close()
				return stages, err
			}
			tickets = append(tickets, ticket)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return stages, err
		}
		for _, name := range []string{"demo-tap.png", "demo-tap.mp4", "voice-note-demo.m4a"} {
			data, err := demoMedia.ReadFile("media/" + name)
			if err != nil {
				return stages, err
			}
			staged, err := core.StageFile(ctx, store, org.ID, name, bytes.NewReader(data), core.MediaPolicy(1<<20))
			if err != nil {
				return stages, err
			}
			stages = append(stages, staged)
			file, err := staged.Save(ctx, tx)
			if err != nil {
				return stages, err
			}
			for _, ticket := range tickets {
				if err := property.AttachTicketFile(ctx, tx, org.ID, ticket.id, file.ID, ticket.userID); err != nil {
					return stages, err
				}
			}
		}
	}
	return stages, nil
}
