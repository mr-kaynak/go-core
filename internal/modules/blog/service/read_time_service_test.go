package service

import (
	"strings"
	"testing"
)

func TestReadTimeService_Calculate_EmptyText(t *testing.T) {
	svc := NewReadTimeService(200)

	got := svc.Calculate("")
	if got != 0 {
		t.Errorf("Calculate(\"\") = %d, want 0", got)
	}
}

func TestReadTimeService_Calculate_WhitespaceOnly(t *testing.T) {
	svc := NewReadTimeService(200)

	got := svc.Calculate("   \t\n  ")
	if got != 0 {
		t.Errorf("Calculate(whitespace only) = %d, want 0", got)
	}
}

func TestReadTimeService_Calculate_ShortText_MinimumOneMinute(t *testing.T) {
	svc := NewReadTimeService(200)

	got := svc.Calculate("one two three four five")
	if got != 1 {
		t.Errorf("Calculate(short text) = %d, want 1 (minimum 1 minute)", got)
	}
}

func TestReadTimeService_Calculate_SingleWord_MinimumOneMinute(t *testing.T) {
	svc := NewReadTimeService(200)

	got := svc.Calculate("hello")
	if got != 1 {
		t.Errorf("Calculate(single word) = %d, want 1", got)
	}
}

func TestReadTimeService_Calculate_ExactWPM(t *testing.T) {
	svc := NewReadTimeService(200)

	words200 := strings.Repeat("word ", 200)
	got := svc.Calculate(words200)
	if got != 1 {
		t.Errorf("Calculate(200 words at 200wpm) = %d, want 1", got)
	}
}

func TestReadTimeService_Calculate_CeilingDivision(t *testing.T) {
	svc := NewReadTimeService(200)

	words201 := strings.Repeat("word ", 201)
	got := svc.Calculate(words201)
	if got != 2 {
		t.Errorf("Calculate(201 words at 200wpm) = %d, want 2", got)
	}
}

func TestReadTimeService_Calculate_350Words(t *testing.T) {
	svc := NewReadTimeService(200)

	words350 := strings.Repeat("word ", 350)
	got := svc.Calculate(words350)
	if got != 2 {
		t.Errorf("Calculate(350 words at 200wpm) = %d, want 2", got)
	}
}

func TestReadTimeService_Calculate_400Words(t *testing.T) {
	svc := NewReadTimeService(200)

	words400 := strings.Repeat("word ", 400)
	got := svc.Calculate(words400)
	if got != 2 {
		t.Errorf("Calculate(400 words at 200wpm) = %d, want 2", got)
	}
}

func TestReadTimeService_Calculate_1000Words(t *testing.T) {
	svc := NewReadTimeService(200)

	words1000 := strings.Repeat("word ", 1000)
	got := svc.Calculate(words1000)
	if got != 5 {
		t.Errorf("Calculate(1000 words at 200wpm) = %d, want 5", got)
	}
}

func TestReadTimeService_Calculate_CustomWPM(t *testing.T) {
	svc := NewReadTimeService(300)

	words300 := strings.Repeat("word ", 300)
	got := svc.Calculate(words300)
	if got != 1 {
		t.Errorf("Calculate(300 words at 300wpm) = %d, want 1", got)
	}
}

func TestReadTimeService_Calculate_ZeroWPM_DefaultsTo200(t *testing.T) {
	svc := NewReadTimeService(0)

	words400 := strings.Repeat("word ", 400)
	got := svc.Calculate(words400)
	if got != 2 {
		t.Errorf("Calculate(400 words, wpm=0 defaults to 200) = %d, want 2", got)
	}
}

func TestReadTimeService_Calculate_NegativeWPM_DefaultsTo200(t *testing.T) {
	svc := NewReadTimeService(-50)

	words400 := strings.Repeat("word ", 400)
	got := svc.Calculate(words400)
	if got != 2 {
		t.Errorf("Calculate(400 words, wpm=-50 defaults to 200) = %d, want 2", got)
	}
}
