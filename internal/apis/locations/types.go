package locations

import (
	"errors"
	"strings"
	"unicode/utf8"
)

var (
	ErrInvalidInput = errors.New("invalid location search input")
	ErrUpstream     = errors.New("location search unavailable")
	errDependency   = errors.New("missing location search dependency")
)

type Location struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Address  string `json:"address"`
	Location string `json:"location"`
	City     string `json:"city"`
}

type SearchInput struct {
	query string
	city  string
}

func NewSearchInput(query, city string) (SearchInput, error) {
	trimmedQuery, trimmedCity := strings.TrimSpace(query), strings.TrimSpace(city)
	if !utf8.ValidString(trimmedQuery) || !utf8.ValidString(trimmedCity) ||
		utf8.RuneCountInString(trimmedQuery) < 1 || utf8.RuneCountInString(trimmedQuery) > 80 ||
		utf8.RuneCountInString(trimmedCity) > 100 {
		return SearchInput{}, ErrInvalidInput
	}
	return SearchInput{query: trimmedQuery, city: trimmedCity}, nil
}
