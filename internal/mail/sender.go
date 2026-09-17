package mail

import (
	"fmt"
	"net/smtp"
)

type Sender struct {
	Host     string
	Port     string
	Username string
	Password string
	From     string
}

func (s Sender) Send(to, subject, body string) error {
	if s.Host == "" || s.From == "" {
		return fmt.Errorf("SMTP is not configured")
	}
	auth := smtp.PlainAuth("", s.Username, s.Password, s.Host)
	msg := []byte("From: " + s.From + "\r\nTo: " + to + "\r\nSubject: " + subject + "\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body)
	return smtp.SendMail(s.Host+":"+s.Port, auth, s.From, []string{to}, msg)
}
