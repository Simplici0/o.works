package pricing

import (
	"math"
	"testing"
)

func nearlyEqual(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Fatalf("%s = %v, want %v", name, got, want)
	}
}

func TestCalculate_QuantityGreaterThanOne(t *testing.T) {
	item := ItemInput{
		Grams:        200,
		PrintMinutes: 30,
		LaborMinutes: 10,
		Quantity:     3,
		CostPerKg:    20,
	}
	global := GlobalInput{
		MachineHourlyRate: 60,
		LaborPerMinute:    0.5,
	}

	result := Calculate(item, global)

	nearlyEqual(t, "materialCost", result.Breakdown.MaterialCost, 4)
	nearlyEqual(t, "machineCost", result.Breakdown.MachineCost, 30)
	nearlyEqual(t, "laborCost", result.Breakdown.LaborCost, 5)
	nearlyEqual(t, "subtotal", result.Breakdown.Subtotal, 117)
	nearlyEqual(t, "total", result.Totals.Total, 117)
}

func TestCalculate_WastePercent_ZeroAndFive(t *testing.T) {
	item := ItemInput{Grams: 1000, Quantity: 1, CostPerKg: 10}

	withoutWaste := Calculate(item, GlobalInput{WastePercent: 0})
	withWaste := Calculate(item, GlobalInput{WastePercent: 5})

	nearlyEqual(t, "withoutWaste materialCost", withoutWaste.Breakdown.MaterialCost, 10)
	nearlyEqual(t, "withWaste materialCost", withWaste.Breakdown.MaterialCost, 10.5)
	nearlyEqual(t, "withoutWaste total", withoutWaste.Totals.Total, 10)
	nearlyEqual(t, "withWaste total", withWaste.Totals.Total, 10.5)
}

func TestCalculate_MarginPercent_ZeroAndThirty(t *testing.T) {
	item := ItemInput{Grams: 1000, Quantity: 1, CostPerKg: 10}

	withoutMargin := Calculate(item, GlobalInput{MarginPercent: 0})
	withMargin := Calculate(item, GlobalInput{MarginPercent: 30})

	nearlyEqual(t, "withoutMargin margin", withoutMargin.Breakdown.Margin, 0)
	nearlyEqual(t, "withMargin margin", withMargin.Breakdown.Margin, 3)
	nearlyEqual(t, "withoutMargin total", withoutMargin.Totals.Total, 10)
	nearlyEqual(t, "withMargin total", withMargin.Totals.Total, 13)
}

func TestCalculate_TaxEnabledOnAndOff(t *testing.T) {
	item := ItemInput{Grams: 1000, Quantity: 1, CostPerKg: 10}
	global := GlobalInput{MarginPercent: 30, TaxPercent: 16}

	withoutTax := Calculate(item, global)
	withTax := Calculate(item, GlobalInput{MarginPercent: 30, TaxEnabled: true, TaxPercent: 16})

	nearlyEqual(t, "withoutTax tax", withoutTax.Breakdown.Tax, 0)
	nearlyEqual(t, "withTax tax", withTax.Breakdown.Tax, 2.08)
	nearlyEqual(t, "withoutTax total", withoutTax.Totals.Total, 13)
	nearlyEqual(t, "withTax total", withTax.Totals.Total, 15.08)
}

func TestCalculate_OverheadFixedAndPercent(t *testing.T) {
	item := ItemInput{Grams: 500, PrintMinutes: 60, LaborMinutes: 15, Quantity: 2, CostPerKg: 20}
	global := GlobalInput{
		MachineHourlyRate: 30,
		LaborPerMinute:    1,
		OverheadFixed:     10,
		OverheadPercent:   20,
	}

	result := Calculate(item, global)

	nearlyEqual(t, "subtotal", result.Breakdown.Subtotal, 110)
	nearlyEqual(t, "overhead", result.Breakdown.Overhead, 32)
	nearlyEqual(t, "total", result.Totals.Total, 142)
}
