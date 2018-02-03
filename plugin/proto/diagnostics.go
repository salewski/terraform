package proto

import (
	"errors"

	"github.com/hashicorp/terraform/tfdiags"
)

func NewDiagnostic(d tfdiags.Diagnostic) *Diagnostic {
	result := &Diagnostic{}
	switch d.Severity() {
	case 'W':
		result.Level = Diagnostic_WARNING
	case 'E':
		result.Level = Diagnostic_ERROR
	}

	desc := d.Description()
	result.Summary = desc.Summary
	result.Detail = desc.Detail
	return result
}

func NewDiagnostics(ds tfdiags.Diagnostics) []*Diagnostic {
	var result []*Diagnostic
	for _, d := range ds {
		result = append(result, NewDiagnostic(d))
	}
	return result
}

func TFDiagnostics(ds []*Diagnostic) tfdiags.Diagnostics {
	var result tfdiags.Diagnostics
	for _, d := range ds {
		switch d.Level {
		case Diagnostic_WARNING:
			result = result.Append(tfdiags.SimpleWarning(d.Summary))
		case Diagnostic_ERROR:
			result = result.Append(errors.New(d.Summary))
		}
	}
	return result
}
