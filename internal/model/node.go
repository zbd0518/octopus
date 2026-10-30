package model

import "time"

// Node is a user-managed outbound connector. Credentials and sessions are not portable.
type Node struct {
	ID        int       `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name" gorm:"size:200;not null"`
	Enabled   bool      `json:"enabled" gorm:"not null"`
	TokenHash string    `json:"-" gorm:"size:64;index"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
