package main

import "testing"

func TestParseDestinations(t *testing.T) {
	tests := []struct {
		name      string
		in        string
		wantFB    bool
		wantTh    bool
		wantError bool
	}{
		{name: "instagram only", in: "instagram", wantFB: false, wantTh: false},
		{name: "facebook", in: "instagram,facebook", wantFB: true, wantTh: false},
		{name: "threads", in: "threads", wantFB: false, wantTh: true},
		{name: "both", in: "instagram,facebook,threads", wantFB: true, wantTh: true},
		{name: "invalid", in: "instagram,tiktok", wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fb, th, err := parseDestinations(tt.in)
			if tt.wantError {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fb != tt.wantFB {
				t.Errorf("facebook=%v, want %v", fb, tt.wantFB)
			}
			if th != tt.wantTh {
				t.Errorf("threads=%v, want %v", th, tt.wantTh)
			}
		})
	}
}
