package main

import "testing"

func TestParseTimeRangeAmPmTypo(t *testing.T) {
	cases := []struct{ in, open, close string }{
		{"10:00PM-7:30PM", "10:00", "19:30"},
		{"10:00AM-7:30AM", "10:00", "19:30"},
		{"10:00AM-7:30PM", "10:00", "19:30"},
		{"11:30-3:30PM", "11:30", "15:30"},
	}
	for _, c := range cases {
		o, cl, err := parseTimeRange(c.in)
		if err != nil || o != c.open || cl != c.close {
			t.Errorf("%q: got %s-%s err=%v, want %s-%s", c.in, o, cl, err, c.open, c.close)
		}
	}
	if _, _, err := parseTimeRange("11:00PM-1:00AM"); err == nil {
		t.Errorf("expected error for implausible range")
	}
}
