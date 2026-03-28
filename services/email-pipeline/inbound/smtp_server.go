package inbound

import (
	"context"
	"io"
	"log/slog"
	"net"

	"github.com/emersion/go-smtp"

	"agentmail/pkg/config"
)

// Backend implements smtp.Backend, creating a new Session for each connection.
type Backend struct {
	enqueuer *Enqueuer
}

// Session implements smtp.Session, accumulating envelope fields before
// calling the enqueuer when Data arrives.
type Session struct {
	backend  *Backend
	remoteIP net.IP // extracted from the TCP connection at session start
	helo     string // EHLO/HELO hostname from the connecting MTA
	from     string
	to       []string
}

// NewSession satisfies smtp.Backend.
func (b *Backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	var ip net.IP
	if addr, ok := c.Conn().RemoteAddr().(*net.TCPAddr); ok {
		ip = addr.IP
	}
	return &Session{
		backend:  b,
		remoteIP: ip,
		helo:     c.Hostname(),
	}, nil
}

// Mail records the MAIL FROM address.
func (s *Session) Mail(from string, opts *smtp.MailOptions) error {
	s.from = from
	return nil
}

// Rcpt accumulates RCPT TO addresses.
func (s *Session) Rcpt(to string, opts *smtp.RcptOptions) error {
	s.to = append(s.to, to)
	return nil
}

// Data reads the message body, runs SPF, stores to S3, and enqueues a Kafka job.
// The SMTP session is released as soon as the enqueue succeeds.
func (s *Session) Data(r io.Reader) error {
	if err := s.backend.enqueuer.Enqueue(context.Background(), s.remoteIP, s.helo, s.from, s.to, r); err != nil {
		slog.Error("inbound enqueue error", "from", s.from, "error", err)
		return err
	}
	return nil
}

// Reset clears the session state between messages on the same connection.
func (s *Session) Reset() {
	s.from = ""
	s.to = nil
}

// Logout is a no-op.
func (s *Session) Logout() error { return nil }

// NewSMTPServer builds and configures an smtp.Server from cfg and enqueuer.
func NewSMTPServer(cfg *config.Config, enqueuer *Enqueuer) *smtp.Server {
	b := &Backend{enqueuer: enqueuer}
	server := smtp.NewServer(b)
	server.Addr = cfg.SMTP.ListenAddr
	server.Domain = cfg.SMTP.Hostname
	server.MaxMessageBytes = 25 * 1024 * 1024 // 25 MB
	server.MaxRecipients = 50
	server.AllowInsecureAuth = true // use TLS in production
	return server
}
