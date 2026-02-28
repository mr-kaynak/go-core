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

	// 5 kelime → (5*60)/200 = 1 saniye, minimum 60 saniyeye yükselmeli
	got := svc.Calculate("one two three four five")
	if got != 60 {
		t.Errorf("Calculate(short text) = %d, want 60 (minimum 1 minute)", got)
	}
}

func TestReadTimeService_Calculate_SingleWord_MinimumOneMinute(t *testing.T) {
	svc := NewReadTimeService(200)

	got := svc.Calculate("hello")
	if got != 60 {
		t.Errorf("Calculate(single word) = %d, want 60 (minimum 1 minute)", got)
	}
}

func TestReadTimeService_Calculate_NormalText_CorrectCalculation(t *testing.T) {
	svc := NewReadTimeService(200)

	// 200 kelime → (200*60)/200 = 60 saniye (tam sınırda)
	words200 := strings.Repeat("word ", 200)
	got := svc.Calculate(words200)
	want := 60
	if got != want {
		t.Errorf("Calculate(200 words at 200wpm) = %d, want %d", got, want)
	}
}

func TestReadTimeService_Calculate_400Words(t *testing.T) {
	svc := NewReadTimeService(200)

	// 400 kelime → (400*60)/200 = 120 saniye
	words400 := strings.Repeat("word ", 400)
	got := svc.Calculate(words400)
	want := 120
	if got != want {
		t.Errorf("Calculate(400 words at 200wpm) = %d, want %d", got, want)
	}
}

func TestReadTimeService_Calculate_1000Words(t *testing.T) {
	svc := NewReadTimeService(200)

	// 1000 kelime → (1000*60)/200 = 300 saniye (5 dakika)
	words1000 := strings.Repeat("word ", 1000)
	got := svc.Calculate(words1000)
	want := 300
	if got != want {
		t.Errorf("Calculate(1000 words at 200wpm) = %d, want %d", got, want)
	}
}

func TestReadTimeService_Calculate_CustomWPM(t *testing.T) {
	// 300 wpm okuyucu
	svc := NewReadTimeService(300)

	// 300 kelime → (300*60)/300 = 60 saniye
	words300 := strings.Repeat("word ", 300)
	got := svc.Calculate(words300)
	want := 60
	if got != want {
		t.Errorf("Calculate(300 words at 300wpm) = %d, want %d", got, want)
	}
}

func TestReadTimeService_Calculate_ZeroWPM_DefaultsTo200(t *testing.T) {
	// wpm=0 → varsayılan 200 kullanılmalı
	svc := NewReadTimeService(0)

	// 400 kelime → (400*60)/200 = 120 saniye
	words400 := strings.Repeat("word ", 400)
	got := svc.Calculate(words400)
	want := 120
	if got != want {
		t.Errorf("Calculate(400 words, wpm=0 defaults to 200) = %d, want %d", got, want)
	}
}

func TestReadTimeService_Calculate_NegativeWPM_DefaultsTo200(t *testing.T) {
	// wpm<0 → varsayılan 200 kullanılmalı
	svc := NewReadTimeService(-50)

	words400 := strings.Repeat("word ", 400)
	got := svc.Calculate(words400)
	want := 120
	if got != want {
		t.Errorf("Calculate(400 words, wpm=-50 defaults to 200) = %d, want %d", got, want)
	}
}

func TestReadTimeService_Calculate_ExactMinimumBoundary(t *testing.T) {
	svc := NewReadTimeService(200)

	// 199 kelime → (199*60)/200 = 59 saniye (integer division), minimum 60 olmalı
	words199 := strings.Repeat("word ", 199)
	got := svc.Calculate(words199)
	if got < 60 {
		t.Errorf("Calculate(199 words) = %d, want at least 60 (minimum enforcement)", got)
	}
}

func TestReadTimeService_Calculate_AboveMinimumBoundary(t *testing.T) {
	svc := NewReadTimeService(200)

	// 201 kelime → (201*60)/200 = 60 saniye (integer division), minimum geçildi
	words201 := strings.Repeat("word ", 201)
	got := svc.Calculate(words201)
	if got < 60 {
		t.Errorf("Calculate(201 words) = %d, expected >= 60", got)
	}
}
