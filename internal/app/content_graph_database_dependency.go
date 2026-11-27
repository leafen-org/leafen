package app

type databaseDependencyBindingAuthority struct {
	DataSourceID          string `json:"dataSourceId"`
	PredecessorPropertyID string `json:"predecessorPropertyId"`
	SuccessorPropertyID   string `json:"successorPropertyId"`
	DataKey               string `json:"dataKey"`
	Revision              int    `json:"revision"`
}
