package models

import "time"

type Pairing struct {
	ExpiresAt time.Time
	CreatedAt time.Time
	Code      string
	SenderID  string
	Channel   string
}
