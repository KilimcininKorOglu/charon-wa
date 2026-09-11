package helper

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

func RenderSpintax(text string) string {
	result := RenderDynamicVariables(text)

	for {
		// Find first closing brace
		end := strings.Index(result, "}")
		if end == -1 {
			break
		}
		// Find the matching opening brace (innermost pair)
		start := strings.LastIndex(result[:end], "{")
		if start == -1 {
			break
		}

		spintax := result[start+1 : end]
		options := strings.Split(spintax, "|")
		// Spintax picks message wording, never a secret.
		// #nosec G404
		chosen := options[rand.Intn(len(options))]

		result = result[:start] + chosen + result[end+1:]
	}
	return result
}

func RenderDynamicVariables(text string) string {
	now := time.Now()

	hour := now.Hour()
	var timeGreeting string
	switch {
	case hour >= 5 && hour < 10:
		timeGreeting = "Good morning"
	case hour >= 10 && hour < 15:
		timeGreeting = "Good afternoon"
	case hour >= 15 && hour < 18:
		timeGreeting = "Good evening"
	default:
		timeGreeting = "Good night"
	}

	dayNames := []string{"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"}
	dayName := dayNames[now.Weekday()]

	monthNames := []string{"", "Januari", "Februari", "Maret", "April", "Mei", "Juni",
		"Juli", "Agustus", "September", "Oktober", "November", "Desember"}
	date := fmt.Sprintf("%d %s %d", now.Day(), monthNames[now.Month()], now.Year())

	result := text
	result = strings.ReplaceAll(result, "{TIME_GREETING}", timeGreeting)
	result = strings.ReplaceAll(result, "{DAY_NAME}", dayName)
	result = strings.ReplaceAll(result, "{DATE}", date)

	return result
}
