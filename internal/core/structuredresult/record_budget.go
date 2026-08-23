package structuredresult

import "fmt"

type RecordBudget struct {
	MaxRecords int
}

type BudgetSelection struct {
	Records            []Record
	Outcome            ParseOutcome
	Completeness       Completeness
	CompletenessReason CompletenessReason
}

func SelectRecordsFailureFirst(records []Record, budget RecordBudget) (BudgetSelection, error) {
	if budget.MaxRecords <= 0 {
		return BudgetSelection{}, fmt.Errorf("invalid structured record budget")
	}
	if len(records) <= budget.MaxRecords {
		return BudgetSelection{
			Records:      append([]Record(nil), records...),
			Outcome:      ParseComplete,
			Completeness: CompletenessComplete,
		}, nil
	}

	mandatory := 0
	for _, record := range records {
		if !isOptionalPassRecord(record) {
			mandatory++
		}
	}
	if mandatory > budget.MaxRecords {
		selected := make([]Record, 0, budget.MaxRecords)
		for _, record := range records {
			if isOptionalPassRecord(record) {
				continue
			}
			selected = append(selected, record)
			if len(selected) == budget.MaxRecords {
				break
			}
		}
		return BudgetSelection{
			Records:      selected,
			Outcome:      ParseBudgetExceeded,
			Completeness: CompletenessPartial,
		}, nil
	}

	selected := make([]bool, len(records))
	optionalRemaining := budget.MaxRecords - mandatory
	for i, record := range records {
		if !isOptionalPassRecord(record) {
			selected[i] = true
			continue
		}
		if optionalRemaining > 0 {
			selected[i] = true
			optionalRemaining--
		}
	}

	out := make([]Record, 0, budget.MaxRecords)
	for i, keep := range selected {
		if keep {
			out = append(out, records[i])
		}
	}
	return BudgetSelection{
		Records:            out,
		Outcome:            ParsePartial,
		Completeness:       CompletenessPartial,
		CompletenessReason: CompletenessReasonPassRecordsElided,
	}, nil
}

func isOptionalPassRecord(record Record) bool {
	return record.RecordKind == RecordTestCase && record.TestCase != nil && record.TestCase.Status == TestPassed
}
