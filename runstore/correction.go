package runstore

// CorrectionMap returns a map from TargetSeq to the latest Correction Record
// targeting it across the provided slice of records.
func CorrectionMap(records []*Record) map[int64]*Record {
	corrections := make(map[int64]*Record)
	for _, r := range records {
		if r != nil && r.Correction != nil && r.Correction.TargetSeq > 0 {
			corrections[r.Correction.TargetSeq] = r
		}
	}
	return corrections
}

// EffectiveRecords returns a copy of records with corrections applied:
// any historical record that was amended by a later correction record adopts
// the corrected measurement figures (rungs, floor loss, reference, integrity, finding),
// while retaining its original sequence number and timestamp. Retrospective correction
// records themselves are not duplicated as distinct measurement timestamps in the series.
//
// If no corrections exist in records, EffectiveRecords returns a copy of the slice.
func EffectiveRecords(records []*Record) []*Record {
	if len(records) == 0 {
		return nil
	}

	corrections := CorrectionMap(records)
	if len(corrections) == 0 {
		out := make([]*Record, len(records))
		copy(out, records)
		return out
	}

	var out []*Record
	for _, r := range records {
		if r == nil {
			continue
		}
		// A correction record amends an earlier record in the chain;
		// do not treat the correction entry itself as a separate measurement point in time.
		if r.Correction != nil && r.Correction.TargetSeq > 0 {
			continue
		}

		if corr, ok := corrections[r.Seq]; ok {
			// Clone the correction record and anchor it at the original record's seq & time
			effective := *corr
			effective.Seq = r.Seq
			effective.RecordedAt = r.RecordedAt
			effective.PrevHash = r.PrevHash
			out = append(out, &effective)
		} else {
			out = append(out, r)
		}
	}

	return out
}
