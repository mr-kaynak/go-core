package service

import (
	"strings"
)

// ReadTimeService calculates estimated reading time
type ReadTimeService struct {
	wpm int
}

// NewReadTimeService creates a new ReadTimeService with the given words-per-minute rate
func NewReadTimeService(wpm int) *ReadTimeService {
	if wpm <= 0 {
		wpm = 200
	}
	return &ReadTimeService{wpm: wpm}
}

// Calculate returns estimated reading time in seconds for the given plain text
func (s *ReadTimeService) Calculate(plainText string) int {
	words := len(strings.Fields(plainText))
	if words == 0 {
		return 0
	}
	seconds := (words * 60) / s.wpm
	if seconds < 60 {
		seconds = 60 // minimum 1 minute
	}
	return seconds
}
