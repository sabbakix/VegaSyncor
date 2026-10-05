// Package i18n translates user-visible text. English is the source language:
// texts are written in English in the code and wrapped with T / Tf; other
// languages are lookup tables keyed by the English text (see it.go).
package i18n

import (
	"fmt"
	"os"
	"strings"
	"sync/atomic"
)

const (
	English = "en"
	Italian = "it"
	Default = English
)

// Language describes a supported language.
type Language struct {
	Code string // e.g. "en"
	Name string // native name, e.g. "Italiano"
}

// Languages lists the supported languages, in display order.
var Languages = []Language{{English, "English"}, {Italian, "Italiano"}}

var catalogs = map[string]map[string]string{
	Italian: italian,
}

var current atomic.Value

func init() { current.Store(Default) }

// Supported reports whether code is a supported language.
func Supported(code string) bool {
	for _, l := range Languages {
		if l.Code == code {
			return true
		}
	}
	return false
}

// Normalize turns values such as "IT", "it_IT.UTF-8" or "" into a supported code.
func Normalize(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	if i := strings.IndexAny(code, "_.-"); i >= 0 {
		code = code[:i]
	}
	if Supported(code) {
		return code
	}
	return Default
}

// SetLang selects the language used by T and Tf.
func SetLang(code string) { current.Store(Normalize(code)) }

// Lang returns the current language code.
func Lang() string { return current.Load().(string) }

// T translates an English text into the current language.
func T(s string) string {
	if c := catalogs[Lang()]; c != nil {
		if t, ok := c[s]; ok {
			return t
		}
	}
	return s
}

// Tf translates an English format string and formats it.
func Tf(format string, a ...any) string {
	return fmt.Sprintf(T(format), a...)
}

// FromEnv returns the language requested with VEGASYNCOR_LANG, or "".
func FromEnv() string {
	if v := os.Getenv("VEGASYNCOR_LANG"); v != "" {
		return Normalize(v)
	}
	return ""
}

// DateTimeLayout is the full date and time layout of the current language.
func DateTimeLayout() string {
	if Lang() == Italian {
		return "02/01/2006 15:04:05"
	}
	return "2006-01-02 15:04:05"
}

// ShortDateTimeLayout is the compact date and time layout (no year, no seconds).
func ShortDateTimeLayout() string {
	if Lang() == Italian {
		return "02/01 15:04"
	}
	return "Jan 02 15:04"
}
