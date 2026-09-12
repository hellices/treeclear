package config

import (
	"testing"
	"time"
)

func TestDurationUnmarshalText(test *testing.T) {
	testCases := []struct {
		text     string
		expected time.Duration
	}{
		{"30m", 30 * time.Minute},
		{"24h", 24 * time.Hour},
		{"1h30m", 90 * time.Minute},
		{"500ms", 500 * time.Millisecond},
		{"1ns", time.Nanosecond},
		{"7d", 7 * 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"+2d", 2 * 24 * time.Hour},
		{"106751d", 106751 * 24 * time.Hour},
		{"0s", 0},
		{"-30m", -30 * time.Minute},
	}
	for _, testCase := range testCases {
		test.Run(testCase.text, func(test *testing.T) {
			actual := Duration{Duration: time.Second}
			if err := actual.UnmarshalText([]byte(testCase.text)); err != nil {
				test.Fatal(err)
			}
			if actual.Duration != testCase.expected {
				test.Fatalf("UnmarshalText(%q) = %s, want %s", testCase.text, actual.Duration, testCase.expected)
			}
		})
	}
}

func TestDurationRejectsInvalidTextWithoutMutation(test *testing.T) {
	for _, text := range []string{
		"", " ", "1", "later", "7dd", "1.5d", "0d", "-1d", " 7d", "7d ",
		"9223372036854775808d", "9223372036854775808ns", "999999999999999999999h",
	} {
		test.Run(text, func(test *testing.T) {
			actual := Duration{Duration: time.Minute}
			if err := actual.UnmarshalText([]byte(text)); err == nil {
				test.Fatalf("UnmarshalText(%q) succeeded, want error", text)
			}
			if actual.Duration != time.Minute {
				test.Fatalf("UnmarshalText(%q) changed receiver to %s on error", text, actual.Duration)
			}
		})
	}
}

func TestDurationRejectsWholeDayOverflow(test *testing.T) {
	for _, text := range []string{"106752d", "213504d", "9223372036854775807d"} {
		test.Run(text, func(test *testing.T) {
			actual := Duration{Duration: time.Minute}
			if err := actual.UnmarshalText([]byte(text)); err == nil {
				test.Fatalf("UnmarshalText(%q) succeeded with %s, want overflow error", text, actual.Duration)
			}
			if actual.Duration != time.Minute {
				test.Fatalf("UnmarshalText(%q) changed receiver to %s on error", text, actual.Duration)
			}
		})
	}
}
