package main

import "testing"

func TestGuestRefreshmentValidation(t *testing.T) {
	valid := guestRefreshmentRequest{
		RequestID: "1234567890abcdef", ProductID: 1, Quantity: 2,
		GuestName: "Visiting guest", Reason: "Owner hospitality",
	}
	if !validGuestRefreshment(valid) {
		t.Fatal("valid guest refreshment rejected")
	}
	cases := []guestRefreshmentRequest{
		{RequestID: "short", ProductID: 1, Quantity: 2, GuestName: "Guest", Reason: "Visit"},
		{RequestID: valid.RequestID, ProductID: 0, Quantity: 2, GuestName: "Guest", Reason: "Visit"},
		{RequestID: valid.RequestID, ProductID: 1, Quantity: 0, GuestName: "Guest", Reason: "Visit"},
		{RequestID: valid.RequestID, ProductID: 1, Quantity: 1001, GuestName: "Guest", Reason: "Visit"},
		{RequestID: valid.RequestID, ProductID: 1, Quantity: 2, GuestName: " ", Reason: "Visit"},
		{RequestID: valid.RequestID, ProductID: 1, Quantity: 2, GuestName: "Guest", Reason: " "},
	}
	for i, x := range cases {
		if validGuestRefreshment(x) {
			t.Errorf("invalid case %d accepted", i)
		}
	}
}
