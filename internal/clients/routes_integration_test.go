//go:build integration

package clients_test

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"testing"

	"shigoto/internal/auth"
	"shigoto/internal/mail"
	"shigoto/internal/testkit"
)

type mailSink struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (s *mailSink) mailer() *mail.Mailer {
	return &mail.Mailer{From: "shigoto@example.com", Send: func(_ context.Context, m mail.Message) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.sent = append(s.sent, m)
		return nil
	}}
}

func (s *mailSink) messages() []mail.Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]mail.Message(nil), s.sent...)
}

const testPDF = "%PDF-1.4\n% route test\n%%EOF\n"

func TestDocumentRoutesUploadAndSendEmailsClient(t *testing.T) {
	e := testkit.New(t)
	sink := &mailSink{}
	h := e.Handler(testkit.Mailer(sink.mailer()))
	org, owner := e.OwnerSession("acme", h)
	c := e.Contact(org, testkit.Kind("client"), testkit.Name("Client One"))
	e.PortalUser("client1@example.com", org, auth.Client, c.ID)

	base := fmt.Sprintf("/api/v1/orgs/acme/contacts/%d/documents", c.ID)
	bad := owner.UploadForm(base+"?title=MSA&kind=signature", "notes.txt", []byte("hello"), nil)
	if bad.Code != http.StatusUnprocessableEntity {
		t.Fatalf("text upload = %d %s, want 422", bad.Code, bad.Body)
	}
	if n := e.Count("client_documents", "true"); n != 0 {
		t.Fatalf("documents after bad upload = %d", n)
	}
	missing := owner.UploadForm(base+"?kind=signature", "msa.pdf", []byte(testPDF), nil)
	if missing.Code != http.StatusUnprocessableEntity || !strings.Contains(string(missing.Body), "title is required") {
		t.Fatalf("no title = %d %s, want 422 title is required", missing.Code, missing.Body)
	}

	up := owner.UploadForm(base+"?title=MSA&kind=signature&category=general", "msa.pdf", []byte(testPDF), nil)
	if up.Code != http.StatusCreated {
		t.Fatalf("upload = %d %s", up.Code, up.Body)
	}
	var doc struct {
		ID     int64  `json:"id"`
		Status string `json:"status"`
		File   struct {
			MIME string `json:"mime"`
		} `json:"file"`
	}
	up.JSON(&doc)
	if doc.Status != "draft" || doc.File.MIME != "application/pdf" {
		t.Fatalf("doc = %+v", doc)
	}

	sent := owner.Do("POST", fmt.Sprintf("/api/v1/orgs/acme/documents/%d/send", doc.ID), nil)
	if sent.Code != http.StatusOK {
		t.Fatalf("send = %d %s", sent.Code, sent.Body)
	}
	msgs := sink.messages()
	if len(msgs) != 1 || msgs[0].To != "client1@example.com" || msgs[0].Subject != "Document to sign: MSA" ||
		!strings.Contains(msgs[0].Text, fmt.Sprintf("https://shigoto.example/portal/acme/documents/%d", doc.ID)) {
		t.Fatalf("emails = %+v", msgs)
	}

	again := owner.Do("POST", fmt.Sprintf("/api/v1/orgs/acme/documents/%d/send", doc.ID), nil)
	if again.Code != http.StatusConflict {
		t.Fatalf("resend = %d %s, want 409", again.Code, again.Body)
	}
	file := owner.Do("GET", fmt.Sprintf("/api/v1/orgs/acme/documents/%d/file", doc.ID), nil)
	if file.Code != http.StatusOK || string(file.Body) != testPDF {
		t.Fatalf("file = %d", file.Code)
	}
	cert := owner.Do("GET", fmt.Sprintf("/api/v1/orgs/acme/documents/%d/certificate", doc.ID), nil)
	if cert.Code != http.StatusNotFound {
		t.Fatalf("certificate before signing = %d, want 404", cert.Code)
	}
}

func TestDocumentRoutesBookkeeperCannotUpload(t *testing.T) {
	e := testkit.New(t)
	h := e.Handler()
	org := e.Org("acme")
	keeper := e.User("keeper@example.com")
	e.Member(keeper, org, auth.Bookkeeper)
	c := e.Contact(org, testkit.Kind("client"))
	s := e.Login(keeper, h)
	r := s.UploadForm(fmt.Sprintf("/api/v1/orgs/acme/contacts/%d/documents?title=X&kind=shared", c.ID), "x.pdf", []byte(testPDF), nil)
	if r.Code != http.StatusForbidden {
		t.Fatalf("bookkeeper upload = %d, want 403", r.Code)
	}
}
