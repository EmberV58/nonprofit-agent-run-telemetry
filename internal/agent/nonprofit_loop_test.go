package agent

import "testing"

func TestDecide(t *testing.T) {
	complete := RunInput{
		DonorName:     "Mina Patel",
		DonorEmail:    "mina@example.org",
		DonationCents: 7500,
		VolunteerName: "Luis Chen",
		ReminderAt:    "2026-10-03T09:00:00Z",
		CampaignName:  "Autumn pantry drive",
	}

	tests := []struct {
		name        string
		mutate      func(*RunInput)
		wantProcess bool
		wantReason  string
	}{
		{name: "complete row enters agent loop", mutate: func(*RunInput) {}, wantProcess: true, wantReason: "work item is complete"},
		{name: "missing donor email is held", mutate: func(in *RunInput) { in.DonorEmail = "" }, wantReason: "donor contact is incomplete"},
		{name: "zero donation is held", mutate: func(in *RunInput) { in.DonationCents = 0 }, wantReason: "donation amount must be positive"},
		{name: "missing reminder time is held", mutate: func(in *RunInput) { in.ReminderAt = "" }, wantReason: "volunteer reminder is incomplete"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := complete
			tt.mutate(&input)
			got := Decide(input)
			if got.Process != tt.wantProcess || got.Reason != tt.wantReason {
				t.Fatalf("Decide() = %+v, want process=%v reason=%q", got, tt.wantProcess, tt.wantReason)
			}
		})
	}
}
