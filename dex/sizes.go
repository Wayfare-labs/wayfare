package dex

import "github.com/shopspring/decimal"

// DefaultSizes is the ladder used when a caller does not specify one.
//
// It spans four orders of magnitude deliberately. The bottom rung exists to
// isolate the structural floor: at 0.1 units price impact is negligible, so
// whatever loss remains is the corridor's spread rather than its depth. The
// top rung exists to expose exhaustion. A ladder covering only realistic
// remittance sizes would show a bad number without showing which of the two
// causes produced it.
//
// Both the route ladder and the depth metric share this single definition so
// they measure the same corridor at the same sizes.
var DefaultSizes = []decimal.Decimal{
	decimal.RequireFromString("0.1"),
	decimal.NewFromInt(1),
	decimal.NewFromInt(5),
	decimal.NewFromInt(10),
	decimal.NewFromInt(25),
	decimal.NewFromInt(50),
	decimal.NewFromInt(100),
	decimal.NewFromInt(250),
	decimal.NewFromInt(500),
	decimal.NewFromInt(1000),
	decimal.NewFromInt(2500),
	decimal.NewFromInt(5000),
}
