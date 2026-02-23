package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed locales/*.json
var localeFS embed.FS

var (
	translations map[string]map[string]string
	currentLang  string
	mu           sync.RWMutex
)

func init() {
	translations = make(map[string]map[string]string)
	loadLocale("en")
	loadLocale("tr")
	currentLang = "en"
}

func loadLocale(lang string) {
	data, err := localeFS.ReadFile(fmt.Sprintf("locales/%s.json", lang))
	if err != nil {
		return
	}
	var msgs map[string]string
	if err := json.Unmarshal(data, &msgs); err != nil {
		return
	}
	translations[lang] = msgs
}

// SetLanguage changes the active language
func SetLanguage(lang string) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := translations[lang]; ok {
		currentLang = lang
	}
}

// T returns the translation for the given key
func T(key string) string {
	mu.RLock()
	defer mu.RUnlock()
	if msgs, ok := translations[currentLang]; ok {
		if val, ok := msgs[key]; ok {
			return val
		}
	}
	// Fallback to English
	if msgs, ok := translations["en"]; ok {
		if val, ok := msgs[key]; ok {
			return val
		}
	}
	return key
}

// GetLocale returns all translations for a given language
func GetLocale(lang string) map[string]string {
	mu.RLock()
	defer mu.RUnlock()
	if msgs, ok := translations[lang]; ok {
		return msgs
	}
	return translations["en"]
}

// AvailableLanguages returns supported language codes
func AvailableLanguages() []string {
	return []string{"en", "tr"}
}
