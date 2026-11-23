package clients

import (
	"context"
	"fmt"
	"html"
	"strings"

	"shigoto/internal/mail"
)

// NotifySent emails the client's portal login that document docID needs
// their signature. A nil mailer (email disabled) sends nothing.
func NotifySent(ctx context.Context, m *mail.Mailer, baseURL, orgSlug, to, title string, docID int64) error {
	if m == nil {
		return nil
	}
	link := fmt.Sprintf("%s/portal/%s/documents/%d", strings.TrimRight(baseURL, "/"), orgSlug, docID)
	return m.Send(ctx, mail.Message{
		To:      to,
		Subject: "Document to sign: " + title,
		Text:    fmt.Sprintf("%s is ready for your signature.\n\nReview and sign it here:\n%s\n", title, link),
		HTML: fmt.Sprintf(`<p>%s is ready for your signature.</p><p><a href="%s">Review and sign</a></p>`,
			html.EscapeString(title), html.EscapeString(link)),
	})
}

// NotifySigned emails staff (to) that clientName signed title. A nil
// mailer (email disabled) or no recipients sends nothing.
func NotifySigned(ctx context.Context, m *mail.Mailer, to []string, baseURL, orgSlug, clientName, title string, contactID int64) error {
	if m == nil {
		return nil
	}
	link := fmt.Sprintf("%s/%s/contacts/%d", strings.TrimRight(baseURL, "/"), orgSlug, contactID)
	var first error
	for _, addr := range to {
		err := m.Send(ctx, mail.Message{
			To:      addr,
			Subject: clientName + " signed " + title,
			Text:    fmt.Sprintf("%s signed %s.\n\nThe signed document and its certificate are here:\n%s\n", clientName, title, link),
			HTML: fmt.Sprintf(`<p>%s signed %s.</p><p><a href="%s">View the client</a></p>`,
				html.EscapeString(clientName), html.EscapeString(title), html.EscapeString(link)),
		})
		if err != nil && first == nil {
			first = err
		}
	}
	return first
}
