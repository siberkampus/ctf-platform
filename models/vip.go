package models

import (
	"time"
)

// VIPPurchase - VIP satın alımları (vip_purchases tablosu ile uyumlu)
type VIPPurchase struct {
	ID            int       `json:"id"`
	UserID        int       `json:"user_id"`
	Package       string    `json:"package"` // monthly, yearly, lifetime
	Price         float64   `json:"price"`
	PaymentMethod string    `json:"payment_method"`
	PurchasedAt   time.Time `json:"purchased_at"`
	ExpiryDate    time.Time `json:"expiry_date"`

	// Join için
	Username string `json:"username,omitempty"`
	Email    string `json:"email,omitempty"`
}

// CampaignCode - Kampanya kodları (campaign_codes tablosu ile uyumlu)
type CampaignCode struct {
	ID              int       `json:"id"`
	Code            string    `json:"code"`
	DiscountPercent int       `json:"discount_percent"`
	ExpiresAt       time.Time `json:"expires_at"`
	IsActive        bool      `json:"is_active"`
}
