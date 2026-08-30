package retrieval

import (
	"sort"
)

const defaultRRFK = 60

// RankedList is one ranked result list for RRF fusion.
type RankedList struct {
	Source Source
	Items  []ScoredEntry // ordered best-first
}

// ReciprocalRankFusion merges multiple ranked lists using RRF (k=60).
func ReciprocalRankFusion(lists []RankedList, k int) []ScoredEntry {
	if k <= 0 {
		k = defaultRRFK
	}
	scores := map[string]float64{}
	sources := map[string]Source{}
	for _, list := range lists {
		for rank, item := range list.Items {
			if item.EntryID == "" {
				continue
			}
			rrf := 1.0 / (float64(k) + float64(rank+1))
			scores[item.EntryID] += rrf
			if _, ok := sources[item.EntryID]; !ok {
				sources[item.EntryID] = list.Source
			}
		}
	}
	out := make([]ScoredEntry, 0, len(scores))
	for id, score := range scores {
		out = append(out, ScoredEntry{EntryID: id, Score: score, Source: sources[id]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].EntryID < out[j].EntryID
	})
	return out
}

// HitsToRankedList converts vector hits to ranked entries (dedupe by entry_id, keep best score).
func HitsToRankedList(hits []Hit, source Source) RankedList {
	best := map[string]float64{}
	order := []string{}
	for _, h := range hits {
		id := EntryIDFromHit(h)
		if id == "" {
			id = h.ID
		}
		if prev, ok := best[id]; !ok || h.Score > prev {
			if !ok {
				order = append(order, id)
			}
			best[id] = h.Score
		}
	}
	items := make([]ScoredEntry, 0, len(best))
	for _, id := range order {
		items = append(items, ScoredEntry{EntryID: id, Score: best[id], Source: source})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].Score != items[j].Score {
			return items[i].Score > items[j].Score
		}
		return items[i].EntryID < items[j].EntryID
	})
	return RankedList{Source: source, Items: items}
}
