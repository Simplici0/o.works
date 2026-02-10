package pricing

// ItemInput represents item-level inputs used to estimate manufacturing costs.
type ItemInput struct {
	Grams        float64
	PrintMinutes float64
	LaborMinutes float64
	Quantity     float64
	CostPerKg    float64
}

// GlobalInput represents global pricing parameters shared across calculations.
type GlobalInput struct {
	MachineHourlyRate  float64
	LaborPerMinute     float64
	OverheadFixed      float64
	OverheadPercent    float64
	FailureRatePercent float64
	WastePercent       float64
	MarginPercent      float64
	TaxEnabled         bool
	TaxPercent         float64
	PackagingCost      float64
	ShippingCost       float64
}

// Breakdown contains all intermediate and line-item values of the pricing calculation.
type Breakdown struct {
	MaterialCost      float64
	MachineCost       float64
	LaborCost         float64
	Subtotal          float64
	Overhead          float64
	FailureInsurance  float64
	PackagingCost     float64
	ShippingCost      float64
	Margin            float64
	Tax               float64
}

// Totals contains roll-up values from the pricing calculation.
type Totals struct {
	Total float64
}

// Result groups the full pricing output, including detailed breakdown and totals.
type Result struct {
	Breakdown Breakdown
	Totals    Totals
}

// Calculate computes pricing values from item-specific and global inputs.
func Calculate(item ItemInput, global GlobalInput) Result {
	materialCost := (item.Grams / 1000.0) * item.CostPerKg * (1.0 + global.WastePercent/100.0)
	machineCost := (item.PrintMinutes / 60.0) * global.MachineHourlyRate
	laborCost := item.LaborMinutes * global.LaborPerMinute

	subtotal := (materialCost + machineCost + laborCost) * item.Quantity
	overhead := global.OverheadFixed + subtotal*(global.OverheadPercent/100.0)
	failureInsurance := subtotal * (global.FailureRatePercent / 100.0)
	margin := (global.MarginPercent / 100.0) * (subtotal + overhead + failureInsurance)

	tax := 0.0
	if global.TaxEnabled {
		tax = (global.TaxPercent / 100.0) * (subtotal + overhead + failureInsurance + margin)
	}

	total := subtotal + overhead + failureInsurance + global.PackagingCost + global.ShippingCost + margin + tax

	return Result{
		Breakdown: Breakdown{
			MaterialCost:     materialCost,
			MachineCost:      machineCost,
			LaborCost:        laborCost,
			Subtotal:         subtotal,
			Overhead:         overhead,
			FailureInsurance: failureInsurance,
			PackagingCost:    global.PackagingCost,
			ShippingCost:     global.ShippingCost,
			Margin:           margin,
			Tax:              tax,
		},
		Totals: Totals{Total: total},
	}
}
