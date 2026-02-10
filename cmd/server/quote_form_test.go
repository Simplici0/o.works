package main

import (
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestParseQuoteFormValues_Success(t *testing.T) {
	form := url.Values{}
	form.Set("material_id", "1")
	form.Set("shipping_id", "2")
	form.Set("packaging_id", "3")
	form.Set("grams", "120")
	form.Set("printMinutes", "95")
	form.Set("laborMinutes", "15")
	form.Set("quantity", "2")
	form.Set("wastePercent", "7")
	form.Set("marginPercent", "35")
	form.Set("taxEnabled", "1")
	form.Set("taxPercent", "19")

	req := httptest.NewRequest("POST", "/quote/calc", nil)
	req.Form = form

	values, err := parseQuoteFormValues(req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if values.MaterialID != 1 || values.ShippingID != 2 || values.PackagingID != 3 {
		t.Fatalf("unexpected ids: %+v", values)
	}
	if !values.TaxEnabled {
		t.Fatalf("expected tax enabled")
	}
}

func TestParseQuoteFormValues_InvalidMaterial(t *testing.T) {
	form := url.Values{}
	form.Set("material_id", "0")
	form.Set("grams", "120")
	form.Set("printMinutes", "95")
	form.Set("laborMinutes", "15")
	form.Set("quantity", "2")
	form.Set("wastePercent", "7")
	form.Set("marginPercent", "35")
	form.Set("taxPercent", "19")

	req := httptest.NewRequest("POST", "/quote/calc", nil)
	req.Form = form

	if _, err := parseQuoteFormValues(req); err == nil {
		t.Fatalf("expected validation error")
	}
}

func TestParseQuoteFormValues_InvalidNumbers(t *testing.T) {
	form := url.Values{}
	form.Set("material_id", "1")
	form.Set("grams", "abc")
	form.Set("printMinutes", "95")
	form.Set("laborMinutes", "15")
	form.Set("quantity", "2")
	form.Set("wastePercent", "7")
	form.Set("marginPercent", "35")
	form.Set("taxPercent", "19")

	req := httptest.NewRequest("POST", "/quote/calc", nil)
	req.Form = form

	if _, err := parseQuoteFormValues(req); err == nil {
		t.Fatalf("expected numeric validation error")
	}
}
