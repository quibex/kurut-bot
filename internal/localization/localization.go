package localization

import (
	"embed"
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed translations/*.yaml
var translationsFS embed.FS

type Service struct {
	translations map[string]map[string]interface{}
}

func NewService() (*Service, error) {
	s := &Service{
		translations: make(map[string]map[string]interface{}),
	}

	languages := []string{"ru", "ky"}
	for _, lang := range languages {
		data, err := translationsFS.ReadFile(fmt.Sprintf("translations/%s.yaml", lang))
		if err != nil {
			return nil, fmt.Errorf("read %s translations: %w", lang, err)
		}

		var translations map[string]interface{}
		if err := yaml.Unmarshal(data, &translations); err != nil {
			return nil, fmt.Errorf("parse %s translations: %w", lang, err)
		}

		s.translations[lang] = translations
	}

	return s, nil
}

// Get retrieves a translation by key for the given language
// Key format: "section.subsection.key" or "section.key"
// Params can contain placeholders like {{name}}, {{amount}}, etc.
func (s *Service) Get(lang, key string, params map[string]interface{}) string {
	if lang == "" {
		lang = "ru"
	}

	langTranslations, ok := s.translations[lang]
	if !ok {
		langTranslations = s.translations["ru"]
	}

	parts := strings.Split(key, ".")
	var current interface{} = langTranslations

	for _, part := range parts {
		if m, ok := current.(map[string]interface{}); ok {
			current = m[part]
		} else {
			return key
		}
	}

	text, ok := current.(string)
	if !ok {
		return key
	}

	return s.replacePlaceholders(text, params)
}

func (s *Service) replacePlaceholders(text string, params map[string]interface{}) string {
	if params == nil {
		return text
	}

	result := text
	for key, value := range params {
		placeholder := fmt.Sprintf("{{%s}}", key)
		result = strings.ReplaceAll(result, placeholder, fmt.Sprint(value))
	}

	return result
}


