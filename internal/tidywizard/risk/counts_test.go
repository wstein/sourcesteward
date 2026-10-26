// SPDX-License-Identifier: EUPL-1.2
package risk

import (
	"reflect"
	"testing"
)

func TestWorstLevelAndCounts(t *testing.T) {
	for _, tc := range []struct {
		name   string
		in     []Assessment
		want   Level
		counts Counts
	}{
		{"empty", nil, Kept, Counts{}},
		{"green", []Assessment{{Level: Green}}, Green, Counts{Green: 1}},
		{"yellow", []Assessment{{Level: Green}, {Level: Yellow}}, Yellow, Counts{Green: 1, Yellow: 1}},
		{"orange", []Assessment{{Level: Orange}, {Level: Green}, {Level: Yellow}}, Orange, Counts{Green: 1, Yellow: 1, Orange: 1}},
		{"red", []Assessment{{Level: Orange}, {Level: Red}, {Level: Green}}, Red, Counts{Green: 1, Orange: 1, Red: 1}},
		{"kept", []Assessment{{Level: Kept}}, Kept, Counts{Kept: 1}},
		{"unknown", []Assessment{{Level: "future"}, {Level: Green}}, Red, Counts{Red: 1, Green: 1}},
		{"empty level", []Assessment{{}}, Red, Counts{Red: 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Worst(tc.in); got != tc.want {
				t.Fatalf("worst %s want %s", got, tc.want)
			}
			if got := Count(tc.in); !reflect.DeepEqual(got, tc.counts) {
				t.Fatalf("counts %+v want %+v", got, tc.counts)
			}
		})
	}
}

func TestGroupLevelCountsAndOrder(t *testing.T) {
	rows := []Row{
		riskRow("o1", "a", "/tmp/orange-one", Green), riskRow("o2", "a", "/tmp/orange-two", Yellow), riskRow("o3", "a", "/tmp/orange-three", Orange),
		riskRow("g1", "a", "/tmp/green-one", Green), riskRow("g2", "a", "/tmp/green-two", Green), riskRow("g3", "a", "/tmp/green-three", Green),
		riskRow("y1", "a", "/tmp/yellow-one", Yellow), riskRow("y2", "a", "/tmp/yellow-two", Green), riskRow("y3", "a", "/tmp/yellow-three", Green), riskRow("y4", "a", "/tmp/yellow-four", Green),
	}
	groups := GroupRows(rows)
	if len(groups) != 3 {
		t.Fatalf("got %+v", groups)
	}
	if groups[0].Level != Green || groups[1].Level != Yellow || groups[2].Level != Orange {
		t.Fatalf("worst risk order: %+v", groups)
	}
	if groups[1].Counts != (Counts{Green: 3, Yellow: 1}) || groups[2].Counts != (Counts{Green: 1, Yellow: 1, Orange: 1}) {
		t.Fatalf("group counts: %+v", groups)
	}
	// Equal risk sorts larger groups first, independent of family names.
	for i := range rows {
		rows[i].Risk.Level = Green
	}
	groups = GroupRows(rows)
	if len(groups[0].Rows) != 4 {
		t.Fatalf("size order: %+v", groups)
	}
}
