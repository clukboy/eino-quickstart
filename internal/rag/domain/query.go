package domain

type Query struct {
	Text string

	Filters []Filter

	Signals QuerySignals

	Options QueryOptions
}

type QuerySignals struct {
	Terms    []string
	Entities []Entity
	Intents  []string
	Topics   []string
	Language string
}

type Entity struct {
	Type     string
	Value    string
	Score    float64
	Metadata Metadata
}

type FilterOperator string

const (
	FilterEqual        FilterOperator = "eq"
	FilterNotEqual     FilterOperator = "neq"
	FilterIn           FilterOperator = "in"
	FilterNotIn        FilterOperator = "not_in"
	FilterContains     FilterOperator = "contains"
	FilterPrefix       FilterOperator = "prefix"
	FilterGreaterThan  FilterOperator = "gt"
	FilterGreaterEqual FilterOperator = "gte"
	FilterLessThan     FilterOperator = "lt"
	FilterLessEqual    FilterOperator = "lte"
)

type Filter struct {
	Field    string
	Operator FilterOperator
	Value    any
}

type QueryOptions struct {
	TopK int

	ScoreThreshold float64

	UseDense    bool
	UseSparse   bool
	UseKeyword  bool
	UseMetadata bool
	UseExact    bool

	UseRerank bool

	IncludeDebug bool
}
