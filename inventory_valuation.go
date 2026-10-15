package main

import (
	"errors"
	"math/big"
)

var errInventoryValuation = errors.New("invalid inventory valuation")

// weightedAverageIssueValue calculates the value of units leaving inventory.
// The last units consume the entire remaining balance, eliminating rounding drift.
func weightedAverageIssueValue(stock int64, balance int64, quantity int64) (int64, error) {
	if stock <= 0 || balance < 0 || quantity <= 0 || quantity > stock {
		return 0, errInventoryValuation
	}

	if quantity == stock {
		return balance, nil
	}

	numerator := new(big.Int).Mul(
		big.NewInt(balance),
		big.NewInt(quantity),
	)

	denominator := big.NewInt(stock)

	// Round to nearest integer, with positive halves rounded upward.
	half := new(big.Int).Div(new(big.Int).Set(denominator), big.NewInt(2))
	numerator.Add(numerator, half)

	result := new(big.Int).Div(numerator, denominator)

	if !result.IsInt64() {
		return 0, errInventoryValuation
	}

	value := result.Int64()

	if value < 0 || value > balance {
		return 0, errInventoryValuation
	}

	return value, nil
}

// inventoryPurchaseCost calculates the exact cost of received stock
// without overflowing int64 multiplication.
func inventoryPurchaseCost(quantity int64, unitCost int64) (int64, error) {
	if quantity <= 0 || unitCost < 0 {
		return 0, errInventoryValuation
	}

	result := new(big.Int).Mul(
		big.NewInt(quantity),
		big.NewInt(unitCost),
	)

	if !result.IsInt64() {
		return 0, errInventoryValuation
	}

	return result.Int64(), nil
}

// inventoryPurchaseValue adds a purchase's actual cost to the verified balance.
func inventoryPurchaseValue(
	currentBalance int64,
	quantity int64,
	unitCost int64,
) (int64, error) {
	if currentBalance < 0 || quantity <= 0 || unitCost < 0 {
		return 0, errInventoryValuation
	}

	addition := new(big.Int).Mul(
		big.NewInt(quantity),
		big.NewInt(unitCost),
	)

	result := new(big.Int).Add(
		big.NewInt(currentBalance),
		addition,
	)

	if !result.IsInt64() {
		return 0, errInventoryValuation
	}

	return result.Int64(), nil
}

// proportionalRefundCost allocates historical sale cost to a refund.
// previouslyRefundedCost is the cost already restored for earlier refunds.
// Refunding the final remaining quantity restores the exact remaining cost.
func proportionalRefundCost(
	originalQuantity int64,
	originalCost int64,
	previouslyRefundedQuantity int64,
	previouslyRefundedCost int64,
	refundQuantity int64,
) (int64, error) {
	if originalQuantity <= 0 ||
		originalCost < 0 ||
		previouslyRefundedQuantity < 0 ||
		previouslyRefundedQuantity >= originalQuantity ||
		previouslyRefundedCost < 0 ||
		previouslyRefundedCost > originalCost ||
		refundQuantity <= 0 ||
		refundQuantity > originalQuantity-previouslyRefundedQuantity {
		return 0, errInventoryValuation
	}

	remainingQuantity := originalQuantity - previouslyRefundedQuantity
	remainingCost := originalCost - previouslyRefundedCost

	return weightedAverageIssueValue(
		remainingQuantity,
		remainingCost,
		refundQuantity,
	)
}
