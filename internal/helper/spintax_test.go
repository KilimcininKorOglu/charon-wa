package helper

import (
	"slices"
	"strings"
	"testing"
)

func TestRenderSpintaxNoSpintax(t *testing.T) {
	input := "Hello World"
	result := RenderSpintax(input)
	if result != input {
		t.Errorf("RenderSpintax(%q) = %q, want %q", input, result, input)
	}
}

func TestRenderSpintaxSimple(t *testing.T) {
	input := "{Hello|Hi|Hey}"
	result := RenderSpintax(input)
	valid := []string{"Hello", "Hi", "Hey"}
	found := slices.Contains(valid, result)
	if !found {
		t.Errorf("RenderSpintax(%q) = %q, expected one of %v", input, result, valid)
	}
}

func TestRenderSpintaxNested(t *testing.T) {
	input := "{Good {morning|evening}|Hi}"
	result := RenderSpintax(input)
	// Should produce one of: "Good morning", "Good evening", "Hi"
	if !strings.HasPrefix(result, "Good ") && result != "Hi" {
		t.Errorf("RenderSpintax(%q) = %q, unexpected result", input, result)
	}
}

func TestRenderSpintaxEmpty(t *testing.T) {
	result := RenderSpintax("")
	if result != "" {
		t.Errorf("RenderSpintax(\"\") = %q, want empty", result)
	}
}
