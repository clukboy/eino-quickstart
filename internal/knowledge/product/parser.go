package product

import (
	"fmt"
	"github.com/goccy/go-json"
)

type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) Parse(data []byte) (*Product, error) {
	var product Product

	if err := json.Unmarshal(data, &product); err != nil {
		return nil, fmt.Errorf("parse product: %w", err)
	}

	return &product, nil
}
